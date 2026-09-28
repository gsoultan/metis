package bpmn_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A worker could fetch an external task and report on it, and nothing else.
// Work that outlasted the lock it fetched with was offered to the next worker
// to ask while the first was still doing it, and the step ran twice. The
// worker holding a lock can now extend it; anybody else, and the holder once
// the lock has run out, is refused.

const (
	lockHolder    = "worker-1"
	anotherWorker = "worker-2"
	oneMinuteMS   = int64(time.Minute / time.Millisecond)
	tenMinutesMS  = 10 * oneMinuteMS
	oneDayMS      = int64(24 * time.Hour / time.Millisecond)
)

// chargeLockedBy starts a charge-card instance and has worker fetch its step
// under a one-minute lock.
func chargeLockedBy(t *testing.T, h engineHarness, worker string) *entities.ExternalTask {
	t.Helper()
	if _, err := h.svc.StartProcess(h.Ctx(), h.projID, "charge-card", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	fetched, err := h.svc.FetchAndLock(h.Ctx(), "charge", worker, 1, oneMinuteMS)
	if err != nil || len(fetched) != 1 {
		t.Fatalf("fetch: %d tasks, %v", len(fetched), err)
	}
	return fetched[0]
}

// storedLock reads a task's lock as the database holds it.
func storedLock(t *testing.T, h engineHarness, id uuid.UUID) (string, time.Time) {
	t.Helper()
	m, err := h.repo.ExternalTask().Get(h.Ctx(), id)
	if err != nil {
		t.Fatalf("read the task: %v", err)
	}
	if m.LockExpiration == nil {
		return m.WorkerID, time.Time{}
	}
	return m.WorkerID, *m.LockExpiration
}

// runOut moves a task's lock a minute into the past, as the clock would.
func runOut(t *testing.T, h engineHarness, id uuid.UUID) {
	t.Helper()
	m, err := h.repo.ExternalTask().Get(h.Ctx(), id)
	if err != nil {
		t.Fatalf("read the task: %v", err)
	}
	past := time.Now().Add(-time.Minute).UTC()
	m.LockExpiration = &past
	if err := h.repo.ExternalTask().Update(h.Ctx(), m); err != nil {
		t.Fatalf("run the lock out: %v", err)
	}
}

// requireRefusedAsLost asserts an extension was refused as a lock the worker
// no longer holds: a 400, whose words tell the worker what to do next.
func requireRefusedAsLost(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "fetch it again") {
		t.Fatalf("got %v; want a refusal telling the worker to fetch the task again", err)
	}
}

func TestTheWorkerHoldingAnExternalTasksLockCanExtendIt(t *testing.T) {
	h := newEngineHarness(t, "Extend Lock Project")
	h.deploy(t, chargeProcess(h.projID))
	task := chargeLockedBy(t, h, lockHolder)
	_, fetchedUntil := storedLock(t, h, task.ID)

	asked := time.Now()
	until, err := h.svc.ExtendLock(h.Ctx(), task.ID, lockHolder, tenMinutesMS)
	if err != nil {
		t.Fatalf("the worker holding the lock could not extend it: %v", err)
	}

	// Ten minutes from the extension, not from the fetch.
	if early, late := asked.Add(10*time.Minute-time.Second), time.Now().Add(10*time.Minute+time.Second); until.Before(early) || until.After(late) {
		t.Fatalf("extended until %v; want ten minutes from now, between %v and %v", until, early, late)
	}
	worker, stored := storedLock(t, h, task.ID)
	if worker != lockHolder || !stored.Equal(until) {
		t.Fatalf("the lock is %q's until %v; want %q's until %v, as the extension answered", worker, stored, lockHolder, until)
	}
	if !stored.After(fetchedUntil) {
		t.Fatalf("the lock still runs out at %v, where the fetch put it at %v", stored, fetchedUntil)
	}
	if others, err := h.svc.FetchAndLock(h.Ctx(), "charge", anotherWorker, 1, oneMinuteMS); err != nil || len(others) != 0 {
		t.Fatalf("a task whose lock was just extended was offered to another worker: %d, %v", len(others), err)
	}
}

func TestAnotherWorkerCannotExtendALockItDoesNotHold(t *testing.T) {
	h := newEngineHarness(t, "Extend Lock Rival Project")
	h.deploy(t, chargeProcess(h.projID))
	task := chargeLockedBy(t, h, lockHolder)
	_, before := storedLock(t, h, task.ID)

	_, err := h.svc.ExtendLock(h.Ctx(), task.ID, anotherWorker, tenMinutesMS)
	requireRefusedAsLost(t, err)

	if worker, after := storedLock(t, h, task.ID); worker != lockHolder || !after.Equal(before) {
		t.Fatalf("after a refused extension the lock is %q's until %v; want it left %q's until %v", worker, after, lockHolder, before)
	}
}

func TestALockThatHasRunOutCannotBeExtended(t *testing.T) {
	h := newEngineHarness(t, "Extend Lock Expired Project")
	h.deploy(t, chargeProcess(h.projID))
	task := chargeLockedBy(t, h, lockHolder)
	runOut(t, h, task.ID)
	_, before := storedLock(t, h, task.ID)

	_, err := h.svc.ExtendLock(h.Ctx(), task.ID, lockHolder, tenMinutesMS)
	requireRefusedAsLost(t, err)

	if _, after := storedLock(t, h, task.ID); !after.Equal(before) {
		t.Fatalf("a refused extension moved the lock from %v to %v", before, after)
	}
}

// The duration is how long from now, in milliseconds: more than none, and no
// more than a day, the longest any lock may be.
func TestALockIsExtendedForSomeTimeAndForNoMoreThanADay(t *testing.T) {
	h := newEngineHarness(t, "Extend Lock Bounds Project")
	h.deploy(t, chargeProcess(h.projID))
	task := chargeLockedBy(t, h, lockHolder)

	for _, tc := range []struct {
		name     string
		worker   string
		duration int64
	}{
		{name: "no time at all", worker: lockHolder, duration: 0},
		{name: "a negative time", worker: lockHolder, duration: -oneMinuteMS},
		{name: "longer than a day", worker: lockHolder, duration: oneDayMS + 1},
		{name: "no worker named", worker: "", duration: tenMinutesMS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			worker, before := storedLock(t, h, task.ID)
			if _, err := h.svc.ExtendLock(h.Ctx(), task.ID, tc.worker, tc.duration); !errors.Is(err, apierr.ErrInvalidArgument) {
				t.Fatalf("got %v; want the request refused as invalid", err)
			}
			if after, until := storedLock(t, h, task.ID); after != worker || !until.Equal(before) {
				t.Fatalf("a refused extension changed the lock from %q until %v to %q until %v", worker, before, after, until)
			}
		})
	}

	until, err := h.svc.ExtendLock(h.Ctx(), task.ID, lockHolder, oneDayMS)
	if err != nil {
		t.Fatalf("extending by exactly a day: %v", err)
	}
	if left := time.Until(until); left < 23*time.Hour || left > 25*time.Hour {
		t.Fatalf("extended by a day and the lock runs out in %v", left)
	}
}

// The worker id is not a credential. Another organization's worker using the
// same one is told the task does not exist, as for anything of another
// tenant's, and the lock is left alone.
func TestAnotherOrganizationsWorkerIsToldTheTaskDoesNotExist(t *testing.T) {
	h := newEngineHarness(t, "Extend Lock Tenant Project")
	h.deploy(t, chargeProcess(h.projID))
	task := chargeLockedBy(t, h, lockHolder)
	_, before := storedLock(t, h, task.ID)

	other, err := h.svc.CreateOrganization(t.Context(), "Another Org", "")
	if err != nil {
		t.Fatalf("create another organization: %v", err)
	}
	outsider := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: other.ID.String()})
	// With a project of its own, so its scope is not empty and the refusal
	// comes from the task being outside it.
	if _, err := h.svc.CreateProject(outsider, other.ID, "Another Project", ""); err != nil {
		t.Fatalf("create another organization's project: %v", err)
	}

	_, err = h.svc.ExtendLock(outsider, task.ID, lockHolder, tenMinutesMS)
	if !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("another organization extending the task: got %v; want not found", err)
	}
	if worker, after := storedLock(t, h, task.ID); worker != lockHolder || !after.Equal(before) {
		t.Fatalf("another organization's extension changed the lock to %q's until %v", worker, after)
	}
}

// The failure extending exists to prevent, arriving late: the lock ran out,
// another worker fetched the task, and only then did the first worker ask for
// more time. It must not get the task back.
func TestALateExtensionCannotTakeBackATaskAnotherWorkerFetched(t *testing.T) {
	h := newEngineHarness(t, "Extend Lock Late Project")
	h.deploy(t, chargeProcess(h.projID))
	task := chargeLockedBy(t, h, lockHolder)
	runOut(t, h, task.ID)

	taken, err := h.svc.FetchAndLock(h.Ctx(), "charge", anotherWorker, 1, oneMinuteMS)
	if err != nil || len(taken) != 1 || taken[0].ID != task.ID {
		t.Fatalf("another worker fetching the task whose lock ran out: %d tasks, %v", len(taken), err)
	}
	_, theirs := storedLock(t, h, task.ID)

	_, err = h.svc.ExtendLock(h.Ctx(), task.ID, lockHolder, tenMinutesMS)
	requireRefusedAsLost(t, err)

	if worker, after := storedLock(t, h, task.ID); worker != anotherWorker || !after.Equal(theirs) {
		t.Fatalf("after the late extension the lock is %q's until %v; want it left %q's until %v", worker, after, anotherWorker, theirs)
	}
}

// The same at the same moment. Once the lock has run out, however the first
// worker's extension and the second worker's fetch interleave, the extension
// is refused and the task is the second worker's, on the second worker's lock.
func TestAnExtensionRacingAnotherWorkersFetchAfterTheLockRanOutNeverWins(t *testing.T) {
	h := newEngineHarness(t, "Extend Lock Race Project")
	h.deploy(t, chargeProcess(h.projID))

	const rounds = 10
	for round := range rounds {
		task := chargeLockedBy(t, h, lockHolder)
		runOut(t, h, task.ID)

		var (
			extendErr, fetchErr error
			taken               []*entities.ExternalTask
			wg                  sync.WaitGroup
		)
		start := make(chan struct{})
		wg.Go(func() {
			<-start
			_, extendErr = h.svc.ExtendLock(h.Ctx(), task.ID, lockHolder, tenMinutesMS)
		})
		wg.Go(func() {
			<-start
			taken, fetchErr = h.svc.FetchAndLock(h.Ctx(), "charge", anotherWorker, 1, oneMinuteMS)
		})
		close(start)
		wg.Wait()

		if fetchErr != nil || len(taken) != 1 || taken[0].ID != task.ID {
			t.Fatalf("round %d: the other worker fetching the task whose lock ran out: %d tasks, %v", round, len(taken), fetchErr)
		}
		if !errors.Is(extendErr, apierr.ErrInvalidArgument) {
			t.Fatalf("round %d: the extension racing the fetch got %v; want it refused", round, extendErr)
		}
		worker, stored := storedLock(t, h, task.ID)
		if worker != anotherWorker || stored.Sub(*taken[0].LockExpiration).Abs() >= time.Microsecond {
			t.Fatalf("round %d: the lock is %q's until %v; want %q's until %v, as their fetch set it",
				round, worker, stored, anotherWorker, *taken[0].LockExpiration)
		}
	}
}
