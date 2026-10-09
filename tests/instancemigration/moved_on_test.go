package instancemigration

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
)

// Deciding work an instance no longer holds.
//
// A migration lists the instances once and then takes them one at a time, so
// the copy it holds of the last one is as old as the whole run. Whether an
// instance is parked on the step being skipped, cancelled at or held at was
// asked of that copy and never again under the instance's lock: a holder who
// completed the step in between had it advanced past a second time, and the
// ledger then said the step was waived.
//
// The hook is the one the stale-write test uses: it runs the completion the
// moment apply has listed the instances, which is the window. No goroutines.

// newRacedFixture is newFixture over a repository whose listing can be made to
// run something as it returns.
func newRacedFixture(t *testing.T) (*fixture, *hookedProcess) {
	t.Helper()
	listing := &hookedProcess{}
	f := newFixtureOver(t, func(repo repositories.Repository) repositories.Repository {
		listing.ProcessRepository = repo.Process()
		return &hookedRepository{Repository: repo, process: listing}
	})
	f.listing = listing
	return f, listing
}

// atApplysListing runs fire when the next migration's apply has listed its
// instances: the plan lists first, the apply second.
func (p *hookedProcess) atApplysListing(fire func()) {
	p.on = p.calls + 2
	p.fire = fire
}

// decideOps is one decision about the operations approval, authorised by dita.
func decideOps(kind servicecontracts.NodeActionKind, reason string) []servicecontracts.MigrationOption {
	return []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: kind, Reason: reason},
		}),
		servicecontracts.WithActor("dita"),
	}
}

// migrateWhileOpsApproves applies a migration that decides the operations
// approval, and has its holder complete that approval after the apply listed
// the instance and before it locked it.
func migrateWhileOpsApproves(t *testing.T, kind servicecontracts.NodeActionKind) (f *fixture, v1, v2 string) {
	t.Helper()
	f, listing := newRacedFixture(t)
	v1, v2 = f.parkedOnOpsApprove(t)
	listing.atApplysListing(func() { f.completeTaskOn(t, "opsApprove", "ollie") })
	if err := f.migrateWithApproval(t, uuidOf(t, v1), uuidOf(t, v2), nil, decideOps(kind, "the role was eliminated")...); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if listing.calls < listing.on {
		t.Fatalf("the hook never reached apply's listing (%d calls); the test is not exercising the window", listing.calls)
	}
	return f, v1, v2
}

// assertOnlyItsHolderActed fails unless the instance is exactly where
// completing the operations approval left it: running the version it was
// started on, one token and one open task on the sales approval, and nothing
// in the ledger, on the trail or among the incidents saying a migration
// decided anything about it.
func (f *fixture) assertOnlyItsHolderActed(t *testing.T, v1 string) {
	t.Helper()
	instance := f.onlyInstance(t)
	if instance.Status != entities.ProcessActive {
		t.Errorf("the instance is %s; it should still be running", instance.Status)
	}
	if instance.Definition == nil || instance.Definition.ID.String() != v1 {
		t.Errorf("the instance is no longer on the version it was started on; the run should have left it for the next one")
	}
	if len(instance.Tokens) != 1 || instance.Tokens[0].Node == nil || instance.Tokens[0].Node.ID != "salesApprove" {
		t.Errorf("the instance holds %d token(s) %+v, want one on the sales approval", len(instance.Tokens), instance.Tokens)
	}
	open := f.openTasks(t)
	if len(open) != 1 || open[0].NodeID() != "salesApprove" {
		t.Errorf("%d task(s) are open (%+v), want the one sales approval the completion opened", len(open), open)
	}
	if rows := f.ledger(t, instance.ID); len(rows) != 0 {
		t.Errorf("the ledger records a decision about a step its holder performed: %+v", rows)
	}
	if incidents, err := f.svc.ListIncidents(f.ctx, instance.ID); err != nil || len(incidents) != 0 {
		t.Errorf("%d incident(s) were raised (err %v) at a step the instance had left", len(incidents), err)
	}
	f.assertNoMigrationEntries(t, instance.ID)
}

// TestASkipOfAStepCompletedAfterTheListingDoesNothing.
//
// Root cause: "is this instance parked on the step" was answered from a read
// taken before the instance's lock and never asked again under it. The skip
// then advanced the instance past a step its holder had already completed —
// two tokens and two open sales approvals — and wrote a waive row and a
// node_skipped entry for an approval that was given.
func TestASkipOfAStepCompletedAfterTheListingDoesNothing(t *testing.T) {
	f, v1, v2 := migrateWhileOpsApproves(t, servicecontracts.NodeActionSkip)
	f.assertOnlyItsHolderActed(t, v1)

	// It was left on the source version, so running the same migration again
	// finds it where it now is: there is nothing to skip, and it moves.
	if err := f.migrateWithApproval(t, uuidOf(t, v1), uuidOf(t, v2), nil, decideOps(servicecontracts.NodeActionSkip, "the role was eliminated")...); err != nil {
		t.Fatalf("the second run: %v", err)
	}
	instance := f.onlyInstance(t)
	if instance.Definition == nil || instance.Definition.ID.String() != v2 {
		t.Fatalf("the second run left the instance on %v, want the new version", instance.Definition)
	}
	if open := f.openTasks(t); len(open) != 1 || open[0].NodeID() != "salesApprove" {
		t.Errorf("after the second run %d task(s) are open (%+v), want the one sales approval", len(open), open)
	}
	if rows := f.ledger(t, instance.ID); len(rows) != 0 {
		t.Errorf("the second run recorded a decision it did not make: %+v", rows)
	}
	for _, told := range f.trailTypes(t, instance.ID) {
		if told == impl.EventNodeSkipped {
			t.Error("the trail says the operations approval was skipped; its holder gave it")
		}
	}
}

// A cancel ends "the instances waiting at this step". One that has left the
// step is not one of them, and is not ended.
func TestACancelAtAStepCompletedAfterTheListingLeavesTheInstanceRunning(t *testing.T) {
	f, v1, _ := migrateWhileOpsApproves(t, servicecontracts.NodeActionCancel)
	f.assertOnlyItsHolderActed(t, v1)
}

// A hold raises an incident at the step for somebody to decide. Raised at a
// step the instance has left, it asks for a decision about nothing, and its
// ledger row says the instance is held where it is not.
func TestAHoldAtAStepCompletedAfterTheListingRaisesNothing(t *testing.T) {
	f, v1, _ := migrateWhileOpsApproves(t, servicecontracts.NodeActionHold)
	f.assertOnlyItsHolderActed(t, v1)
}

// The other half of the question asked under the lock: the instance is still
// running. A skip of one that finished after the listing must not advance it
// again from a step it passed.
func TestASkipOfAnInstanceThatFinishedAfterTheListingDoesNothing(t *testing.T) {
	f, listing := newRacedFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)
	listing.atApplysListing(func() {
		f.completeTaskOn(t, "opsApprove", "ollie")
		f.completeTaskOn(t, "salesApprove", "sasha")
	})
	if err := f.migrateWithApproval(t, uuidOf(t, v1), uuidOf(t, v2), nil, decideOps(servicecontracts.NodeActionSkip, "the role was eliminated")...); err != nil {
		t.Fatalf("apply: %v", err)
	}
	instance := f.onlyInstance(t)
	if instance.Status != entities.ProcessCompleted || len(instance.Tokens) != 0 {
		t.Fatalf("the instance is %s with %d token(s); it finished before the skip reached it", instance.Status, len(instance.Tokens))
	}
	if instance.Definition == nil || instance.Definition.ID.String() != v1 {
		t.Errorf("a finished instance was re-pointed at %v", instance.Definition)
	}
	if open := f.openTasks(t); len(open) != 0 {
		t.Errorf("a finished instance has %d open task(s): %+v", len(open), open)
	}
	if rows := f.ledger(t, instance.ID); len(rows) != 0 {
		t.Errorf("the ledger records a skip of a step its holder performed: %+v", rows)
	}
	f.assertNoMigrationEntries(t, instance.ID)
}
