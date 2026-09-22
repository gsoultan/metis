package instancemigration

import (
	"sync"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A completion arriving at the same moment as a migration.
//
// Both rewrite the same instance: one re-points the task and re-derives who may
// do it, the other authorises somebody and advances the token. Without the
// instance lock being taken before either decides, both act on the state they
// found and the loser's write lands on top of the winner's.

// TestACompletionRacingAMigrationLosesCleanly.
//
// This is an invariant check, not a reproduction. The window it guards is a few
// statements wide and this test does not reliably land inside it — it passes
// against the old ordering too, which is stated here rather than left for
// somebody to discover when they rely on it. What it does do is assert that
// however the two interleave, the result is one of the two legitimate orders
// and never a mixture: a task is never both completed and still waiting, and an
// instance never ends up holding work on a node absent from the version it
// names.
//
// The ordering fix it accompanies is argued rather than demonstrated: deciding
// who may complete a task and then taking the lock means the decision is made
// against a row another transaction is free to rewrite before the write lands.
func TestACompletionRacingAMigrationLosesCleanly(t *testing.T) {
	for attempt := 0; attempt < 8; attempt++ {
		f := newFixture(t)
		v1 := f.deploy(t, "approve")
		if _, err := f.svc.StartProcess(f.ctx, f.project, "expense-approval", nil); err != nil {
			t.Fatalf("start an instance: %v", err)
		}
		v2 := f.deploy(t, "review")

		open := f.openTasks(t)
		if len(open) != 1 {
			t.Fatalf("expected one open task to race over, found %d", len(open))
		}
		taskID := open[0].ID

		var start sync.WaitGroup
		var done sync.WaitGroup
		start.Add(1)
		done.Add(2)

		var completeErr, migrateErr error
		go func() {
			defer done.Done()
			start.Wait()
			completeErr = f.svc.CompleteTask(f.ctx, taskID, "ada", nil)
		}()
		go func() {
			defer done.Done()
			start.Wait()
			migrateErr = f.svc.MigrateInstances(f.ctx, v1, v2, map[string]string{"approve": "review"})
		}()
		start.Done()
		done.Wait()

		// A refusal from either side is a legitimate outcome of losing the
		// race. What must not happen is both succeeding into a mixed state.
		instance := f.onlyInstance(t)
		stillOpen := f.openTasks(t)

		switch {
		case completeErr == nil && instance.Status == entities.ProcessCompleted:
			// The completion won: the instance ran to its end event. It may or
			// may not then have been migrated, but it must hold no open work.
			if len(stillOpen) != 0 {
				t.Fatalf("attempt %d: the instance finished but %d task(s) are still open", attempt, len(stillOpen))
			}
		case completeErr != nil:
			// The migration won and the completion was refused. The task must
			// still be there to do, on the node the new version calls it.
			if len(stillOpen) != 1 {
				t.Fatalf("attempt %d: the completion was refused (%v) but %d task(s) are open",
					attempt, completeErr, len(stillOpen))
			}
			if instance.Definition != nil && instance.Definition.ID == v2 && stillOpen[0].NodeID() != "review" {
				t.Fatalf("attempt %d: the instance is on version 2 but its open task is on %q",
					attempt, stillOpen[0].NodeID())
			}
		default:
			// The completion succeeded and the instance is still running: the
			// only shape that allows is a completion that landed before the
			// migration, leaving nothing open.
			if len(stillOpen) != 0 {
				t.Fatalf("attempt %d: a task was completed and %d task(s) are still open on the same node",
					attempt, len(stillOpen))
			}
		}
		if migrateErr != nil && completeErr != nil {
			t.Fatalf("attempt %d: both sides failed; one of them should have won (migrate=%v complete=%v)",
				attempt, migrateErr, completeErr)
		}
	}
}
