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

// Who held the work a skip took away.
//
// A skip of work somebody does reads the tasks open on the step, for the
// record, and then has the engine end the step, which reads them again and
// tells whoever holds each one. Claiming a task takes the task's row and not
// the instance's, so a claim could commit between the two reads: the ledger
// then said nobody held a task whose withdrawal was announced to the person
// who had just claimed it. A claim committing a moment later still — after the
// engine's read — was told to nobody and recorded for nobody, on a task that
// was cancelled in somebody's hands.
//
// The skip now holds each task's row before it records it, so what it records
// is what the engine then reads, and a claim either came first and is in both
// or waits and is refused.

// skippingApprove is the migration that skips the approval, as dita.
func skippingApprove() []servicecontracts.MigrationOption {
	return []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"approve": {Kind: servicecontracts.NodeActionSkip, Reason: "the purchase was approved by the board instead"},
		}),
		servicecontracts.WithActor("dita"),
	}
}

// waitingOnApprovals starts a parallel approval with one run for each
// approver, and deploys the version that drops the approval.
func (f *fixture) waitingOnApprovals(t *testing.T, approvers ...any) (v1, v2 uuid.UUID) {
	t.Helper()
	v1, err := f.svc.CreateDefinition(f.ctx, repeatingApproval(f.project, entities.UserTask, "parallel", true))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "repeating-approval-parallel",
		map[string]any{"approvers": approvers}); err != nil {
		t.Fatalf("start: %v", err)
	}
	v2, err = f.svc.CreateDefinition(f.ctx, repeatingApproval(f.project, entities.UserTask, "parallel", false))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	return v1, v2
}

// recordedHolders is who a ledger row says held each task its action
// withdrew, by task id: "" where it says nobody did.
func recordedHolders(t *testing.T, row entities.Deviation) map[string]string {
	t.Helper()
	tasks, ok := row.Before["tasks"].(map[string]any)
	if !ok {
		t.Fatalf("the row does not say what the tasks were before it: %v", row.Before)
	}
	holders := make(map[string]string, len(tasks))
	for id, was := range tasks {
		values, ok := was.(map[string]any)
		if !ok {
			t.Fatalf("the row says of task %s: %v", id, was)
		}
		holder, _ := values["assignee"].(string)
		holders[id] = holder
	}
	return holders
}

// assertTheRecordNamesWhoWasTold asks of an action that withdrew the
// approval's tasks that its ledger row and its announcements agree about who
// held the work, and that both agree with the tasks as they were left. The
// tasks are asked as well as the announcements, because the two can agree and
// both be wrong: nobody recorded and nobody told, of a task somebody held.
func (f *fixture) assertTheRecordNamesWhoWasTold(t *testing.T, round string, row entities.Deviation, told []entities.ProcessEvent) {
	t.Helper()
	recorded := recordedHolders(t, row)

	var recordedNames, toldNames []string
	for _, holder := range recorded {
		recordedNames = append(recordedNames, holder)
	}
	for _, event := range told {
		toldNames = append(toldNames, event.Assignee)
	}
	slices.Sort(recordedNames)
	slices.Sort(toldNames)
	if !slices.Equal(recordedNames, toldNames) {
		t.Errorf("%s: the ledger says the withdrawn work was held by %q; the withdrawals were announced to %q",
			round, recordedNames, toldNames)
	}

	all, err := f.svc.ListTasks(f.ctx, f.project)
	if err != nil {
		t.Fatalf("%s: list tasks: %v", round, err)
	}
	withdrawn := 0
	for _, task := range all {
		if task.NodeID() != "approve" || task.Status != entities.TaskCanceled {
			continue
		}
		withdrawn++
		holder, listed := recorded[task.ID.String()]
		if !listed {
			t.Errorf("%s: task %s was withdrawn and the ledger does not list it", round, task.ID)
			continue
		}
		if holder != task.AssigneeUsername() {
			t.Errorf("%s: the ledger says %q held task %s when it was withdrawn; the task says %q",
				round, holder, task.ID, task.AssigneeUsername())
		}
	}
	if withdrawn != len(recorded) {
		t.Errorf("%s: %d task(s) were withdrawn and the ledger lists %d", round, withdrawn, len(recorded))
	}
}

// TestAClaimRacingASkipIsRecordedAsItWasAnnounced.
//
// A probabilistic check, like TestACompletionRacingAMigrationLosesCleanly: the
// window is a statement or two wide and a given round may not land in it. It
// asserts only what holds under every interleaving — each task the skip
// withdrew is recorded with the holder it had, and announced to that holder.
// TestASkipRecordsTheHolderItWaitedFor makes the same interleaving happen
// every time.
func TestAClaimRacingASkipIsRecordedAsItWasAnnounced(t *testing.T) {
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
			migrateErr = f.svc.MigrateInstances(f.ctx, v1, v2, nil, skippingApprove()...)
		})
		for i, task := range open {
			claimer := fmt.Sprintf("claimer-%d", i)
			done.Go(func() {
				start.Wait()
				// Spread over the time the migration takes to reach the step,
				// so some claims land before it, some after, and some as it is
				// there. A claim that loses is refused; that is not a failure.
				time.Sleep(time.Duration(i) * 2 * time.Millisecond)
				_ = f.svc.ClaimTask(testutils.AsOperator(f.ctx, claimer), task.ID, claimer)
			})
		}
		start.Done()
		waitForAll(t, &done, fmt.Sprintf("round %d: the skip and the claims racing it", round))

		if migrateErr != nil {
			t.Fatalf("round %d: the skip failed: %v", round, migrateErr)
		}
		f.assertTheRecordNamesWhoWasTold(t, fmt.Sprintf("round %d", round),
			f.theWaiverOf(t, f.onlyInstance(t).ID, "approve"), watcher.events)
	}
}

// raceWait is how long a migration is given to get where it is going. Far longer
// than it takes; it only bounds a test that has gone wrong.
const raceWait = 20 * time.Second

// waitForAll waits for everything a race started to finish, for no longer
// than raceWait. A race that deadlocks would otherwise hold the whole package
// until its own timeout, and say nothing of which test it was.
func waitForAll(t *testing.T, racing *sync.WaitGroup, what string) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		racing.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(raceWait):
		t.Fatalf("%s did not finish within %s", what, raceWait)
	}
}

// TestASkipRecordsTheHolderItWaitedFor makes the interleaving happen every
// time: carol's claim of one run is held open, uncommitted, the skip is sent,
// and the claim is committed only once the skip is waiting behind it.
//
// The skip has to be waiting for the task's row when it reads who holds the
// task, not afterwards. Reading first and waiting at the write is what it did:
// the row it had read said nobody held the task, so nobody was recorded and
// nobody was told, about a task carol held when it was withdrawn.
func TestASkipRecordsTheHolderItWaitedFor(t *testing.T) {
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

	skipped := make(chan error, 1)
	go func() { skipped <- f.svc.MigrateInstances(f.ctx, v1, v2, nil, skippingApprove()...) }()

	f.waitUntilHeldBehind(t, session, skipped)
	if err := claim.Commit().Error; err != nil {
		t.Fatalf("commit the claim: %v", err)
	}
	committed = true

	select {
	case err := <-skipped:
		if err != nil {
			t.Fatalf("the skip failed once the claim was let go: %v", err)
		}
	case <-time.After(raceWait):
		t.Fatal("the skip did not finish once the claim was let go")
	}

	row := f.theWaiverOf(t, f.onlyInstance(t).ID, "approve")
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

// waitUntilHeldBehind waits for the migration to be waiting for the task the
// session holds. One that finishes instead never waited for the claim, and
// the test has not made the interleaving it is about.
//
// What is looked for is the migration's own backend and no other: one the
// session is blocking that is queued for a row of this test's tasks table.
// The database is shared with every other test that is running, and anything
// that happened to be blocked by the session for another reason would
// otherwise be taken for the migration, and the claim let go too early.
func (f *fixture) waitUntilHeldBehind(t *testing.T, session int, finished <-chan error) {
	t.Helper()
	const queuedForATask = `SELECT count(*) FROM pg_stat_activity a
		 WHERE ? = ANY(pg_blocking_pids(a.pid))
		   AND EXISTS (SELECT 1 FROM pg_locks l
		                 JOIN pg_class c ON c.oid = l.relation
		                 JOIN pg_namespace n ON n.oid = c.relnamespace
		                WHERE l.pid = a.pid AND l.locktype = 'tuple'
		                  AND c.relname = 'tasks' AND n.nspname = current_schema())`
	for deadline := time.Now().Add(raceWait); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		select {
		case err := <-finished:
			t.Fatalf("the migration finished (%v) without waiting for the claim it raced", err)
		default:
		}
		var waiting int
		if err := f.db.Raw(queuedForATask, session).Scan(&waiting).Error; err != nil {
			t.Fatalf("look for the migration waiting behind the claim: %v", err)
		}
		if waiting > 0 {
			return
		}
	}
	t.Fatal("the migration was neither finished nor made to wait for the task")
}
