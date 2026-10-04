package instancemigration

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/endpoints/definition"
)

// An instance another run has already moved.
//
// A client that retries a slow apply runs the same migration twice at once.
// The second run listed the instance while it was still on the old version;
// by the time it holds the instance's lock the first run has moved it. The
// lock was asked whether the instance is still running and never whether it
// is still on the version being migrated from, so the second run applied the
// mapping, and the decisions, to an instance already on the new version.

// migrationEntriesOf counts the instance_migrated entries on a trail.
func (f *fixture) migrationEntriesOf(t *testing.T, instanceID uuid.UUID) int {
	t.Helper()
	moved := 0
	for _, told := range f.trailTypes(t, instanceID) {
		if told == impl.EventInstanceMigrated {
			moved++
		}
	}
	return moved
}

// assertAlreadyMoved fails unless the reason says the instance had already
// been moved, in words and with no id.
func assertAlreadyMoved(t *testing.T, reason string) {
	t.Helper()
	t.Logf("passed over, and told: %s", reason)
	if !strings.Contains(reason, "already") || !strings.Contains(reason, "version 1") || anyID.MatchString(reason) {
		t.Errorf("the reason should say the instance had already been moved off version 1, and carry no id: %q", reason)
	}
}

// TestASecondRunDoesNotMoveAgainAnInstanceTheFirstAlreadyMoved.
//
// Root cause: under the rewrite's lock the instance was asked whether it is
// still running, not whether it is still on the source version.
//
// With a mapping that chains — the operations approval goes to the
// supervisor's review, the supervisor's review to the sales approval — the
// first run puts the quotation at the supervisor's review on version 2. The
// second run, which listed it before that, then applied the same mapping to
// where it now stood: on to the sales approval, the supervisor's review passed
// without anybody performing it, and a second entry saying it was migrated.
func TestASecondRunDoesNotMoveAgainAnInstanceTheFirstAlreadyMoved(t *testing.T) {
	f, listing := newRacedFixture(t)
	first, second := f.parkedOnOpsApprove(t)
	v1, v2 := uuidOf(t, first), uuidOf(t, second)
	chained := map[string]string{"opsApprove": "supervisorReview", "supervisorReview": "salesApprove"}

	// The first run, whole, inside the second's window: after the second has
	// listed the instance and before it locks it.
	listing.atTheEndpointsApplyListing(func() {
		if _, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, chained, servicecontracts.WithActor("dita")); err != nil {
			t.Errorf("the first run: %v", err)
		}
	})
	dryRun := false
	reply, body := f.migrateOverTheEndpoint(t, definition.MigrateInstancesRequest{
		SourceDefinitionID: first, TargetDefinitionID: second, DryRun: &dryRun, NodeMapping: chained,
	})
	reached(t, listing)

	// Where the first run put it, and moved once.
	instance := f.assertWaitingAt(t, v2, "supervisorReview")
	f.assertNothingIsStranded(t)
	if moved := f.migrationEntriesOf(t, instance.ID); moved != 1 {
		t.Errorf("the trail says the instance was migrated %d times; it was moved once", moved)
	}

	// The second run wrote nothing, and says which instance and why.
	if *reply.Applied {
		t.Errorf("applied is true although the second run wrote nothing to any instance: %s", body)
	}
	if len(*reply.PassedOver) != 1 || (*reply.PassedOver)[0].InstanceID != instance.ID.String() {
		t.Fatalf("passed_over is %+v, want the one instance (%s)", *reply.PassedOver, instance.ID)
	}
	assertAlreadyMoved(t, (*reply.PassedOver)[0].Reason)

	// The supervisor still has the review to perform, and the quotation ends
	// as version 2 says it should.
	f.runsToItsEnd(t)
}

// TestADecisionIsNotTakenOnAnInstanceAnotherRunAlreadyMoved.
//
// The decisions had the same gap, and a worse one. A decision names a step,
// and an instance that has been moved can stand on a step of that name on the
// new version: here the mapping sends the operations approval to the sales
// approval, and the migration decides the instances waiting at the sales
// approval on the old version. The first run moves the quotation to the sales
// approval on version 2. The second run then found it "waiting at the sales
// approval", and skipped it there on the old version's graph, or ended it, or
// held it — an instance that was no longer part of the migration at all.
func TestADecisionIsNotTakenOnAnInstanceAnotherRunAlreadyMoved(t *testing.T) {
	for _, kind := range everyNodeAction {
		t.Run(string(kind), func(t *testing.T) {
			f, listing := newRacedFixture(t)
			first, second := f.parkedOnOpsApprove(t)
			v1, v2 := uuidOf(t, first), uuidOf(t, second)
			mapping := map[string]string{"opsApprove": "salesApprove"}
			opts := []servicecontracts.MigrationOption{
				servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
					"salesApprove": {Kind: kind, Reason: "sales approvals in flight are being reviewed"},
				}),
				servicecontracts.WithActor("dita"),
			}
			listing.atApplysListing(func() {
				if _, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, mapping, opts...); err != nil {
					t.Errorf("the first run: %v", err)
				}
			})
			result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, mapping, opts...)
			if err != nil {
				t.Fatalf("the second run: %v", err)
			}
			reached(t, listing)

			// Moved once by the first run, and nothing decided about it.
			instance := f.assertWaitingAt(t, v2, "salesApprove")
			f.assertNothingIsStranded(t)
			if rows := f.ledger(t, instance.ID); len(rows) != 0 {
				t.Errorf("the ledger records a decision about an instance that had already been moved: %+v", rows)
			}
			if incidents, incErr := f.svc.ListIncidents(f.ctx, instance.ID); incErr != nil || len(incidents) != 0 {
				t.Errorf("%d incident(s) were raised (err %v) on an instance that had already been moved", len(incidents), incErr)
			}
			for _, told := range f.trailTypes(t, instance.ID) {
				if told == impl.EventNodeSkipped || told == impl.EventInstanceCancelled || told == impl.EventInstanceHeld {
					t.Errorf("the trail holds a %s entry for an instance that had already been moved", told)
				}
			}
			if moved := f.migrationEntriesOf(t, instance.ID); moved != 1 {
				t.Errorf("the trail says the instance was migrated %d times; it was moved once", moved)
			}

			if result.Changed != 0 {
				t.Errorf("the second run says it acted on %d instance(s); it wrote nothing", result.Changed)
			}
			if len(result.PassedOver) != 1 || result.PassedOver[0].Instance == nil || result.PassedOver[0].Instance.ID != instance.ID {
				t.Fatalf("the second run passed over %+v, want the one instance (%s)", result.PassedOver, instance.ID)
			}
			assertAlreadyMoved(t, result.PassedOver[0].Reason)
			if instance.Status != entities.ProcessActive {
				t.Errorf("the instance is %s; it should still be running", instance.Status)
			}
			f.runsToItsEnd(t)
		})
	}
}
