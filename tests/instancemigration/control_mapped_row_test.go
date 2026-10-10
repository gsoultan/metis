package instancemigration

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
)

// A ledger row and a trail entry are evidence: they say no more than happened,
// and no less.
//
// A control a mapping sends onto a step that is not the same step in place is
// a hold, and an instance waiting at it, once the loss is acknowledged and
// approved, is moved onto the step the mapping names. In one shape that is
// not known to be a loss: the control's id is gone from the new version and
// the mapping sends it to an id the new version adds, which nothing else is
// mapped onto and which is marked as a control — a rename by ids, with the
// step's neighbours changed. The record then says the control was not carried
// across as the same step and that the instance was moved onto the step
// named. In every other shape the control the instance waited at is certainly
// not performed, and the record says what the second administrator was
// shown: it never will be.

// renamedCheck is the second signature under a new id, still a control.
var renamedCheck = step{id: "check", name: "Second signature", control: true}

// nextCheck is another control, which stands after the second signature.
var nextCheck = step{id: "next", name: "Third signature", control: true}

// lostControlOf applies, approved, a migration that maps the control an
// instance waits at, and answers the ledger rows and the migration's entry.
func lostControlOf(t *testing.T, f *fixture, v1, v2 uuid.UUID, instance entities.ProcessInstance,
	mapping map[string]string, acknowledged ...string) ([]entities.Deviation, entities.AuditEntry) {
	t.Helper()
	opts := []servicecontracts.MigrationOption{servicecontracts.WithAcknowledgedHolds(acknowledged...), servicecontracts.WithActor("dita")}
	if result, err := f.applyWithApproval(t, v1, v2, mapping, opts...); err != nil || result.Changed != 1 {
		t.Fatalf("the approved migration: %+v %v", result, err)
	}
	rows := f.ledger(t, instance.ID)
	for _, row := range rows {
		if row.Kind != entities.DeviationControlWaived {
			t.Fatalf("the ledger holds a %s row, want only the controls': %+v", row.Kind, rows)
		}
	}
	return rows, f.entryOf(t, instance.ID, impl.EventInstanceMigrated)
}

// A control renamed by ids, with what stands round it changed: the row says
// where its waiting instance was moved (details.mapped_to), and the trail
// that the control was not carried across as the same step and the instance
// was moved onto the step named. Neither says it will be performed, nor that
// it never will: the record cannot know which.
func TestAControlRenamedWithItsNeighboursChangedIsRecordedAsMovedOntoTheNewStep(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, lined(f.project, controlStep, signStep), lined(f.project, prepareStep, renamedCheck, signStep))
	instance := f.assertWaitingAt(t, v1, "control")

	rows, entry := lostControlOf(t, f, v1, v2, instance, map[string]string{"control": "check"}, "control")
	f.assertWaitingAt(t, v2, "check")
	if len(rows) != 1 || rows[0].Node == nil || rows[0].Node.ID != "control" || rows[0].Details["mapped_to"] != "check" {
		t.Fatalf("the ledger: %+v, want the one row for the control, saying it was mapped to check", rows)
	}
	const moved = " It had not yet passed control, which was not carried across as the same step: it was moved from there onto check, and dita accepted that."
	if !strings.Contains(entry.Narrative, moved) || strings.Contains(entry.Narrative, "never will") {
		t.Fatalf("the trail says\n  %s\nwant it to say\n  %s", entry.Narrative, moved)
	}
	if mapped, _ := entry.Data["controls_mapped_to"].(map[string]any); mapped["control"] != "check" {
		t.Fatalf("the entry's data says %v of where the control was mapped, want control → check", entry.Data["controls_mapped_to"])
	}
}

// Every other shape is a control certainly lost, and is recorded as one: the
// sentence the trail always had, whole, and nothing of a mapping on the row
// or the entry — whatever the step the work was sent to is marked as.
func TestAControlThatIsCertainlyLostIsRecordedAsLostWhateverItIsMappedOnto(t *testing.T) {
	const never = " It had not yet passed %s, and dita accepted that it never will."
	for name, shape := range map[string]struct {
		first, second []step
		reach         []string // steps completed before the migration
		at            string
		mapping       map[string]string
		lost          []string
		lands         string
	}{
		// R2: both versions have both controls, and the mapping sends the
		// first onto the second. The first is still a step of the new
		// version, and this instance is put past it.
		"the old id is still a step of the new version": {
			first: []step{controlStep, nextCheck}, second: []step{controlStep, nextCheck},
			at: "control", mapping: map[string]string{"control": "next"}, lost: []string{"control"}, lands: "next",
		},
		// The new version drops the control, and the instance is put on the
		// control that came after it — which it had to perform anyway.
		"the step it is mapped onto was already a step": {
			first: []step{prepareStep, controlStep, nextCheck}, second: []step{prepareStep, nextCheck},
			reach: []string{"prepare"}, at: "control", mapping: map[string]string{"control": "next"}, lost: []string{"control"}, lands: "next",
		},
		// Two controls become one step: whichever the instance performs
		// there, it does not perform two.
		"two controls are mapped onto one": {
			first: []step{controlStep, nextCheck}, second: []step{renamedCheck},
			at: "control", mapping: map[string]string{"control": "check", "next": "check"}, lost: []string{"control", "next"}, lands: "check",
		},
		// The new version drops the control and the work is sent to a step
		// that is no control at all.
		"it is mapped onto a step that is no control": {
			first: []step{prepareStep, controlStep, signStep}, second: []step{prepareStep, signStep},
			reach: []string{"prepare"}, at: "control", mapping: map[string]string{"control": "sign"}, lost: []string{"control"}, lands: "sign",
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			v1, v2 := f.startedOn(t, lined(f.project, shape.first...), lined(f.project, shape.second...))
			for _, done := range shape.reach {
				f.completeTaskOn(t, done, "ada")
			}
			instance := f.assertWaitingAt(t, v1, shape.at)

			rows, entry := lostControlOf(t, f, v1, v2, instance, shape.mapping, shape.lost...)
			f.assertWaitingAt(t, v2, shape.lands)
			if len(rows) != len(shape.lost) {
				t.Fatalf("the ledger holds %d row(s), want one for each of %v: %+v", len(rows), shape.lost, rows)
			}
			for _, row := range rows {
				if _, said := row.Details["mapped_to"]; said {
					t.Errorf("the row of %s, a control certainly lost, says it was mapped: %v", row.Node.ID, row.Details)
				}
			}
			want := "This instance was moved from version 1 to version 2 of \"lined\" by a migration, authorised by dita. " +
				"Work in progress was re-pointed: " + shape.at + "→" + shape.lands + "."
			if !strings.HasPrefix(entry.Narrative, want) {
				t.Fatalf("the trail begins\n  %s\nwant\n  %s", entry.Narrative, want)
			}
			sentence := strings.Replace(never, "%s", strings.Join(shape.lost, ", "), 1)
			if !strings.Contains(entry.Narrative, sentence) || strings.Contains(entry.Narrative, "not carried across as the same step") {
				t.Fatalf("the trail says\n  %s\nwant the sentence it always had, whole:\n  %s", entry.Narrative, sentence)
			}
			if _, said := entry.Data["controls_mapped_to"]; said {
				t.Fatalf("the entry of a control certainly lost says it was mapped: %v", entry.Data)
			}
		})
	}
}
