package instancemigration

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
)

// A ledger row is evidence, and says no more than happened.
//
// Root cause: a control that a mapping sends onto a step that is not the same
// step in place is a hold — rightly, since nothing can tell a control renamed
// with its neighbours changed from one redirected onto another control — and
// an instance waiting at it, once the loss is acknowledged and approved, is
// moved onto the step the mapping names. The row then written was the row of
// a control dropped, and the trail said the instance "never will" pass it,
// though it stands on a control of the new version and may go on to perform
// exactly that step.

// The row of such a control says where the instance was moved to
// (details.mapped_to), and the trail says what happened: the control was not
// carried across as the same step, and the instance was moved onto the step
// named. Neither says the instance will never perform it.
func TestAControlMappedOntoAnotherStepIsRecordedAsMovedOntoItNotAsLost(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, twoControls(f.project), twoControls(f.project))
	instance := f.assertWaitingAt(t, v1, "c1")
	onto := map[string]string{"c1": "c2"}
	accepted := []servicecontracts.MigrationOption{servicecontracts.WithAcknowledgedHolds("c1"), servicecontracts.WithActor("dita")}

	if result, err := f.applyWithApproval(t, v1, v2, onto, accepted...); err != nil || result.Changed != 1 {
		t.Fatalf("the approved migration: %+v %v", result, err)
	}
	f.assertWaitingAt(t, v2, "c2")
	rows := f.ledger(t, instance.ID)
	if len(rows) != 1 || rows[0].Kind != entities.DeviationControlWaived || rows[0].Node == nil || rows[0].Node.ID != "c1" {
		t.Fatalf("the ledger: %+v, want the one control_waived row for the first check", rows)
	}
	if rows[0].Details["mapped_to"] != "c2" || rows[0].Details["note"] != "maker" {
		t.Fatalf("the row's details are %v; want mapped_to: c2 beside the control's note", rows[0].Details)
	}
	entry := f.entryOf(t, instance.ID, impl.EventInstanceMigrated)
	const moved = " It had not yet passed c1, which was not carried across as the same step: it was moved from there onto c2, and dita accepted that."
	if !strings.Contains(entry.Narrative, moved) || strings.Contains(entry.Narrative, "never will") {
		t.Fatalf("the trail says\n  %s\nwant it to say\n  %s\nand not that the instance never will pass it", entry.Narrative, moved)
	}
	mapped, _ := entry.Data["controls_mapped_to"].(map[string]any)
	if mapped["c1"] != "c2" {
		t.Fatalf("the entry's data says %v of where the control was mapped, want c1 → c2", entry.Data["controls_mapped_to"])
	}
}

// A control the new version does not have is a control dropped, whatever the
// mapping does with the work that waited at it: the instance never will
// perform it, the row says nothing of a mapping, and the trail's sentence is
// the one it always was, to the letter.
func TestAControlTheNewVersionDropsKeepsItsRowAndItsSentence(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, lined(f.project, prepareStep, controlStep, signStep), lined(f.project, prepareStep, signStep))
	f.completeTaskOn(t, "prepare", "ada")
	instance := f.assertWaitingAt(t, v1, "control")
	drop := map[string]string{"control": "sign"}

	if err := f.migrateWithApproval(t, v1, v2, drop,
		servicecontracts.WithAcknowledgedHolds("control"), servicecontracts.WithActor("dita")); err != nil {
		t.Fatalf("the approved migration: %v", err)
	}
	f.assertWaitingAt(t, v2, "sign")
	rows := f.ledger(t, instance.ID)
	if len(rows) != 1 || rows[0].Kind != entities.DeviationControlWaived || rows[0].Node == nil || rows[0].Node.ID != "control" {
		t.Fatalf("the ledger: %+v, want the one control_waived row for the control", rows)
	}
	if _, said := rows[0].Details["mapped_to"]; said {
		t.Fatalf("the row of a control the new version dropped says it was mapped: %v", rows[0].Details)
	}
	entry := f.entryOf(t, instance.ID, impl.EventInstanceMigrated)
	if !strings.Contains(entry.Narrative, " It had not yet passed control, and dita accepted that it never will.") {
		t.Fatalf("the trail no longer says of a dropped control what it always said: %s", entry.Narrative)
	}
	if _, said := entry.Data["controls_mapped_to"]; said {
		t.Fatalf("the entry of a dropped control says it was mapped: %v", entry.Data)
	}
}
