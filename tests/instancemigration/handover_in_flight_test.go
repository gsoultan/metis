package instancemigration

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// A delegation in flight, where the tests beside this one race a claim.
//
// Delegating a task takes the task's row and not its instance, as a claim
// does, so it is not kept out by the lock a skip or a cancellation holds. It
// changes more than a claim: the task goes to the delegate, and whoever
// delegated it becomes its owner, waiting for it back. A task withdrawn then
// is taken from two people. The record has to name the delegate as its
// holder, and the announcement has to reach the delegate and say who was
// waiting for the task — both as the delegation left them, not as the action
// read the task before it.
func TestAWithdrawalRecordsTheDelegationItWaitedFor(t *testing.T) {
	for _, action := range []struct {
		name string
		// kind is what the options decide the step by: a skip waits for a
		// second administrator, a cancellation is one administrator's call.
		kind    servicecontracts.NodeActionKind
		options func() []servicecontracts.MigrationOption
		row     func(f *fixture, t *testing.T, instanceID uuid.UUID) entities.Deviation
	}{
		{"a skip", servicecontracts.NodeActionSkip, skippingApprove, func(f *fixture, t *testing.T, id uuid.UUID) entities.Deviation {
			return f.theWaiverOf(t, id, "approve")
		}},
		{"a cancellation", servicecontracts.NodeActionCancel, cancellingAtApprove, func(f *fixture, t *testing.T, id uuid.UUID) entities.Deviation { return f.theCancellationOf(t, id) }},
	} {
		t.Run(action.name, func(t *testing.T) {
			f := newFixture(t)
			watcher := &withdrawalWatcher{}
			f.dispatcher.Register(watcher)
			v1, v2 := f.waitingOnApprovals(t, "ana", "budi", "citra")
			open := f.openTasks(t)
			if len(open) != 3 {
				t.Fatalf("%d task(s) are open, want three", len(open))
			}
			delegated := open[1].ID

			// carol delegates the task to dave, written and not yet committed.
			delegation := f.db.Begin()
			if delegation.Error != nil {
				t.Fatalf("open the transaction that holds the delegation: %v", delegation.Error)
			}
			committed := false
			t.Cleanup(func() {
				if !committed {
					delegation.Rollback()
				}
			})
			var session int
			if err := delegation.Raw("SELECT pg_backend_pid()").Scan(&session).Error; err != nil {
				t.Fatalf("read the delegation's session: %v", err)
			}
			written := delegation.Exec(`UPDATE tasks SET status = 'delegated', assignee = 'dave', owner = 'carol', delegation_state = 'pending'
				 WHERE id = ?`, delegated)
			if written.Error != nil || written.RowsAffected != 1 {
				t.Fatalf("delegate the task: %d row(s), %v", written.RowsAffected, written.Error)
			}

			acted := make(chan error, 1)
			go func() { acted <- f.migrateDecided(t, action.kind, v1, v2, nil, action.options()...) }()

			f.waitUntilHeldBehind(t, session, acted)
			if err := delegation.Commit().Error; err != nil {
				t.Fatalf("commit the delegation: %v", err)
			}
			committed = true

			select {
			case err := <-acted:
				if err != nil {
					t.Fatalf("%s failed once the delegation was let go: %v", action.name, err)
				}
			case <-time.After(raceWait):
				t.Fatalf("%s did not finish once the delegation was let go", action.name)
			}

			row := action.row(f, t, f.onlyInstance(t).ID)
			recorded := recordedHolders(t, row)
			if got := recorded[delegated.String()]; got != "dave" {
				t.Errorf("the ledger says %q held the task carol had delegated to dave when it was withdrawn; it holds %v", got, recorded)
			}
			toldDave := slices.IndexFunc(watcher.events, func(event entities.ProcessEvent) bool { return event.Assignee == "dave" })
			if toldDave < 0 {
				t.Fatalf("dave held a task that was withdrawn and was not told; %d withdrawal(s) were announced", len(watcher.events))
			}
			if owner := watcher.events[toldDave].Owner; owner != "carol" {
				t.Errorf("the withdrawal of the task carol was waiting to have back names %q as waiting for it, want carol", owner)
			}
			f.assertTheRecordNamesWhoWasTold(t, "after the delegation", row, watcher.events)
		})
	}
}
