package impl

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	handlersimpl "github.com/gsoultan/metis/server/domains/handlers/impl"
	observerimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// The row update is what claims a job, and it carries a five-minute lease. The
// distributed lock is a second, optional gate — and the order between them is
// load-bearing: claim the row first and a refused lock leaves a job marked
// running that no worker is running, idle until the lease expires.
//
// That ordering was wrong until this test existed, and invisibly so, because
// the shipped locker is a Null Object that never refuses. It only becomes
// reachable the moment somebody wires a real one.

// refusingLocker declines every lock, which is what a replica that lost the
// race sees.
type refusingLocker struct{ released []string }

func (l *refusingLocker) TryAcquire(context.Context, string, time.Duration) (bool, error) {
	return false, nil
}

func (l *refusingLocker) Release(_ context.Context, key string) error {
	l.released = append(l.released, key)
	return nil
}

// failingLocker reports an error rather than a clean refusal — a database blip
// on the lock path, which must be just as safe.
type failingLocker struct{ released []string }

func (l *failingLocker) TryAcquire(context.Context, string, time.Duration) (bool, error) {
	return false, errors.New("lock backend unavailable")
}

func (l *failingLocker) Release(_ context.Context, key string) error {
	l.released = append(l.released, key)
	return nil
}

// countingLocker grants the lock and records what it was asked to release, so a
// lost row race can be checked for cleaning up after itself.
type countingLocker struct {
	acquired []string
	released []string
}

func (l *countingLocker) TryAcquire(_ context.Context, key string, _ time.Duration) (bool, error) {
	l.acquired = append(l.acquired, key)
	return true, nil
}

func (l *countingLocker) Release(_ context.Context, key string) error {
	l.released = append(l.released, key)
	return nil
}

// seedPendingJob writes one pending job and returns its id.
func seedPendingJob(t *testing.T, repo repositories.Repository) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	job := models.JobModel{
		Base:      models.Base{ID: models.FromUUID(id)},
		Type:      models.JobTimer,
		Status:    models.JobPending,
		NextRunAt: time.Now().Add(-time.Minute),
	}
	created, err := repo.Job().Create(t.Context(), job)
	if err != nil {
		t.Fatalf("seed job: %v", err)
	}
	return created
}

func jobServiceWithLocker(t *testing.T, locker interface {
	TryAcquire(context.Context, string, time.Duration) (bool, error)
	Release(context.Context, string) error
}) (*jobService, repositories.Repository) {
	t.Helper()
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	engine := NewExecutionEngine(repo, observerimpl.NewEventDispatcher())
	svc := NewJobService(repo, engine, NewConnectorService(repo), locker, handlersimpl.NewErrorBoundaryMatcher())
	js, ok := svc.(*jobService)
	if !ok {
		t.Fatalf("NewJobService returned %T, want *jobService", svc)
	}
	return js, repo
}

func assertJobStillPending(t *testing.T, repo repositories.Repository, id uuid.UUID, why string) {
	t.Helper()
	after, err := repo.Job().Get(t.Context(), id)
	if err != nil {
		t.Fatalf("read job back: %v", err)
	}
	if after.Status != models.JobPending {
		t.Fatalf("%s: job is %q, want pending — it was claimed by a worker that never ran it, and is now idle until the lease expires",
			why, after.Status)
	}
}

func TestRefusedDistributedLockLeavesTheJobClaimable(t *testing.T) {
	locker := &refusingLocker{}
	svc, repo := jobServiceWithLocker(t, locker)
	id := seedPendingJob(t, repo)

	if svc.tryAcquireJobLock(t.Context(), id) {
		t.Fatal("the job was claimed despite the distributed lock being refused")
	}
	assertJobStillPending(t, repo, id, "lock refused")
}

func TestFailingDistributedLockLeavesTheJobClaimable(t *testing.T) {
	locker := &failingLocker{}
	svc, repo := jobServiceWithLocker(t, locker)
	id := seedPendingJob(t, repo)

	if svc.tryAcquireJobLock(t.Context(), id) {
		t.Fatal("the job was claimed despite the lock backend failing")
	}
	assertJobStillPending(t, repo, id, "lock backend failed")
}

// TestLosingTheRowRaceReleasesTheDistributedLock covers the other direction:
// the lock was taken, then another replica won the row. Keeping the lock would
// block the replica that actually owns the job.
func TestLosingTheRowRaceReleasesTheDistributedLock(t *testing.T) {
	locker := &countingLocker{}
	svc, repo := jobServiceWithLocker(t, locker)
	id := seedPendingJob(t, repo)

	// A rival claims the row first, so this worker's row update matches nothing.
	claimed, err := repo.Job().Lock(t.Context(), id, 5*time.Minute, "rival-worker")
	if err != nil || !claimed {
		t.Fatalf("rival claim: claimed=%v err=%v", claimed, err)
	}

	if svc.tryAcquireJobLock(t.Context(), id) {
		t.Fatal("two workers hold the same job")
	}
	if len(locker.released) != 1 || locker.released[0] != "job:"+id.String() {
		t.Fatalf("the lock was not released after losing the row race: released=%v", locker.released)
	}
}

// TestWinningBothGatesClaimsTheJob is the happy path, so the tests above cannot
// pass by making acquisition impossible.
func TestWinningBothGatesClaimsTheJob(t *testing.T) {
	locker := &countingLocker{}
	svc, repo := jobServiceWithLocker(t, locker)
	id := seedPendingJob(t, repo)

	if !svc.tryAcquireJobLock(t.Context(), id) {
		t.Fatal("a free job with a granting locker was not claimed")
	}
	after, err := repo.Job().Get(t.Context(), id)
	if err != nil {
		t.Fatalf("read job back: %v", err)
	}
	if after.Status != models.JobRunning {
		t.Fatalf("claimed job is %q, want running", after.Status)
	}
	if len(locker.released) != 0 {
		t.Fatalf("a successful claim released its lock: %v", locker.released)
	}
}
