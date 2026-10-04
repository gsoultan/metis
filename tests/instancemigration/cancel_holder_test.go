package instancemigration

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/tests/testutils"
)

// Who held the work a cancellation took away.
//
// Cancelling an instance withdraws every task it has open. It read the tasks,
// and then cancelled and announced each from what it had read — and a claim
// takes the task's row and not the instance's, so it is not kept out by the
// lock the cancellation holds. A claim in flight made the cancellation's write
// wait for it and then land on top of it: the task was cancelled in the hands
// of the person who had just claimed it, the ledger said nobody held it, and
// nobody was told.
//
// The cancellation now holds each task's row before it decides what to
// record, as a skip does (see repeating_skip_holder_test.go), so a claim
// either came first and is recorded and announced, or waits and is refused.

// cancellingAtApprove is the migration that cancels the instances waiting at
// the approval, as dita.
func cancellingAtApprove() []servicecontracts.MigrationOption {
	return []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"approve": {Kind: servicecontracts.NodeActionCancel, Reason: "the purchase was withdrawn"},
		}),
		servicecontracts.WithActor("dita"),
	}
}

// theCancellationOf is the one ledger row that says the instance was
// cancelled, and fails the test when there is any other number of them.
func (f *fixture) theCancellationOf(t *testing.T, instanceID uuid.UUID) entities.Deviation {
	t.Helper()
	var cancellations []entities.Deviation
	for _, row := range f.ledger(t, instanceID) {
		if row.Kind == entities.DeviationCancel {
			cancellations = append(cancellations, row)
		}
	}
	if len(cancellations) != 1 {
		t.Fatalf("the ledger holds %d cancellation(s) of the instance, want exactly one", len(cancellations))
	}
	return cancellations[0]
}

// TestAClaimRacingACancellationIsRecordedAsItWasAnnounced.
//
// A probabilistic check, like the one for a skip: it asserts only what holds
// under every interleaving — each task the cancellation withdrew is recorded
// with the holder its row has, and announced to that holder.
// TestACancellationRecordsTheHolderItWaitedFor makes the interleaving happen
// every time.
func TestAClaimRacingACancellationIsRecordedAsItWasAnnounced(t *testing.T) {
	approvers := []any{"ana", "budi", "citra", "dewi", "eko", "fitri"}
	for round := range 12 {
		f := newFixture(t)
		watcher := &withdrawalWatcher{}
		f.dispatcher.Register(watcher)
		v1, v2 := f.waitingOnApprovals(t, approvers...)
		open := f.openTasks(t)
		if len(open) != len(approvers) {
			t.Fatalf("round %d: %d task(s) are open, want one for each of %d approvers", round, len(open), len(approvers))
		}

		var start, done sync.WaitGroup
		start.Add(1)
		var migrateErr error
		done.Go(func() {
			start.Wait()
			migrateErr = f.svc.MigrateInstances(f.ctx, v1, v2, nil, cancellingAtApprove()...)
		})
		for i, task := range open {
			claimer := fmt.Sprintf("claimer-%d", i)
			done.Go(func() {
				start.Wait()
				// Spread over the time the migration takes to reach the
				// instance. A claim that loses is refused; that is not a
				// failure.
				time.Sleep(time.Duration(i) * 2 * time.Millisecond)
				_ = f.svc.ClaimTask(testutils.AsOperator(f.ctx, claimer), task.ID, claimer)
			})
		}
		start.Done()
		done.Wait()

		if migrateErr != nil {
			t.Fatalf("round %d: the cancellation failed: %v", round, migrateErr)
		}
		instance := f.onlyInstance(t)
		if instance.Status != entities.ProcessCancelled {
			t.Fatalf("round %d: the instance is %s, want cancelled", round, instance.Status)
		}
		if still := f.openTasks(t); len(still) != 0 {
			t.Errorf("round %d: the instance was cancelled and %d task(s) are still open", round, len(still))
		}
		f.assertTheRecordNamesWhoWasTold(t, fmt.Sprintf("round %d", round), f.theCancellationOf(t, instance.ID), watcher.events)
	}
}

// TestACancellationRecordsTheHolderItWaitedFor makes the interleaving happen
// every time: carol's claim of one task is held open, uncommitted, the
// cancellation is sent, and the claim is committed only once the cancellation
// is waiting behind it.
func TestACancellationRecordsTheHolderItWaitedFor(t *testing.T) {
	f := newFixture(t)
	watcher := &withdrawalWatcher{}
	f.dispatcher.Register(watcher)
	v1, v2 := f.waitingOnApprovals(t, "ana", "budi", "citra")
	open := f.openTasks(t)
	if len(open) != 3 {
		t.Fatalf("%d task(s) are open, want three", len(open))
	}
	claimed := open[1].ID

	// carol's claim, written and not yet committed: the row is hers to let go.
	claim := f.db.Begin()
	if claim.Error != nil {
		t.Fatalf("open the transaction that holds the claim: %v", claim.Error)
	}
	committed := false
	t.Cleanup(func() {
		if !committed {
			claim.Rollback()
		}
	})
	var session int
	if err := claim.Raw("SELECT pg_backend_pid()").Scan(&session).Error; err != nil {
		t.Fatalf("read the claim's session: %v", err)
	}
	if err := claim.Exec("UPDATE tasks SET status = 'claimed', assignee = 'carol' WHERE id = ?", claimed).Error; err != nil {
		t.Fatalf("claim the task: %v", err)
	}

	cancelled := make(chan error, 1)
	go func() { cancelled <- f.svc.MigrateInstances(f.ctx, v1, v2, nil, cancellingAtApprove()...) }()

	f.waitUntilHeldBehind(t, session, cancelled)
	if err := claim.Commit().Error; err != nil {
		t.Fatalf("commit the claim: %v", err)
	}
	committed = true

	select {
	case err := <-cancelled:
		if err != nil {
			t.Fatalf("the cancellation failed once the claim was let go: %v", err)
		}
	case <-time.After(raceWait):
		t.Fatal("the cancellation did not finish once the claim was let go")
	}

	instance := f.onlyInstance(t)
	if instance.Status != entities.ProcessCancelled {
		t.Fatalf("the instance is %s, want cancelled", instance.Status)
	}
	row := f.theCancellationOf(t, instance.ID)
	recorded := recordedHolders(t, row)
	if got := recorded[claimed.String()]; got != "carol" {
		t.Errorf("the ledger says %q held the task carol had claimed when it was withdrawn; it holds %v", got, recorded)
	}
	toldCarol := slices.ContainsFunc(watcher.events, func(event entities.ProcessEvent) bool {
		return event.Assignee == "carol"
	})
	if !toldCarol {
		t.Errorf("carol held a task that was withdrawn and was not told; %d withdrawal(s) were announced", len(watcher.events))
	}
	f.assertTheRecordNamesWhoWasTold(t, "after the claim", row, watcher.events)
}
