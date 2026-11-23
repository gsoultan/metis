package impl

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// The lease a worker takes on a job is renewed while the job runs, and the
// job's outcome is written only while the worker still holds it. Before this,
// a run that outlasted its lease was claimed and run again by another replica
// while the first was still waiting on its partner, and both recorded an
// outcome.

func claimFor(t *testing.T, repo repositories.Repository, id uuid.UUID, lease time.Duration, worker string) {
	t.Helper()
	claimed, err := repo.Job().Lock(t.Context(), id, lease, worker)
	if err != nil || !claimed {
		t.Fatalf("claim for %s: claimed=%v err=%v", worker, claimed, err)
	}
}

func readJob(t *testing.T, repo repositories.Repository, id uuid.UUID) models.JobModel {
	t.Helper()
	m, err := repo.Job().Get(t.Context(), id)
	if err != nil {
		t.Fatalf("read job: %v", err)
	}
	return m
}

func TestALeaseIsRenewedOnlyForTheWorkerThatHoldsIt(t *testing.T) {
	svc, repo := jobServiceWithLocker(t, &countingLocker{})
	id := seedPendingJob(t, repo)
	claimFor(t, repo, id, time.Minute, svc.workerID)
	before := *readJob(t, repo, id).LockExpires

	held, err := repo.Job().Renew(t.Context(), id, svc.workerID, 10*time.Minute)
	if err != nil || !held {
		t.Fatalf("the holder's renewal: held=%v err=%v", held, err)
	}
	if after := *readJob(t, repo, id).LockExpires; !after.After(before.Add(5 * time.Minute)) {
		t.Fatalf("lease runs to %s after renewal, was %s: not moved", after, before)
	}

	held, err = repo.Job().Renew(t.Context(), id, "another-worker", 10*time.Minute)
	if err != nil || held {
		t.Fatalf("a worker that does not hold the job renewed it: held=%v err=%v", held, err)
	}
}

func TestALeaseThatRanOutIsNotRenewedOnceAnotherWorkerHasTheJob(t *testing.T) {
	svc, repo := jobServiceWithLocker(t, &countingLocker{})
	id := seedPendingJob(t, repo)
	claimFor(t, repo, id, time.Millisecond, svc.workerID)
	time.Sleep(20 * time.Millisecond)
	claimFor(t, repo, id, time.Minute, "rival-worker")

	held, err := repo.Job().Renew(t.Context(), id, svc.workerID, time.Minute)
	if err != nil || held {
		t.Fatalf("the first worker renewed a job the rival had reclaimed: held=%v err=%v", held, err)
	}
	if got := readJob(t, repo, id).LockedBy; got != "rival-worker" {
		t.Fatalf("job held by %q, want the rival", got)
	}
}

func TestAWorkerThatLostItsLeaseCannotCompleteTheJob(t *testing.T) {
	svc, repo := jobServiceWithLocker(t, &countingLocker{})
	id := seedPendingJob(t, repo)
	claimFor(t, repo, id, time.Millisecond, svc.workerID)
	job := adapters.JobEntityAdapter{Model: readJob(t, repo, id)}.ToEntity()
	time.Sleep(20 * time.Millisecond)
	claimFor(t, repo, id, time.Minute, "rival-worker")

	err := repo.UnitOfWork().Do(t.Context(), func(txCtx context.Context) error {
		return svc.completeJob(txCtx, job)
	})
	if !errors.Is(err, repocontracts.ErrJobLeaseLost) {
		t.Fatalf("completing a job another worker holds: err=%v, want ErrJobLeaseLost", err)
	}
	after := readJob(t, repo, id)
	if after.Status != models.JobRunning || after.LockedBy != "rival-worker" {
		t.Fatalf("job is %q held by %q; the late worker's completion was written over the rival's run",
			after.Status, after.LockedBy)
	}
}

func TestTheWorkerHoldingTheJobCompletesIt(t *testing.T) {
	svc, repo := jobServiceWithLocker(t, &countingLocker{})
	id := seedPendingJob(t, repo)
	claimFor(t, repo, id, time.Minute, svc.workerID)
	job := adapters.JobEntityAdapter{Model: readJob(t, repo, id)}.ToEntity()

	if err := repo.UnitOfWork().Do(t.Context(), func(txCtx context.Context) error {
		return svc.completeJob(txCtx, job)
	}); err != nil {
		t.Fatalf("the holder could not complete its job: %v", err)
	}
	if got := readJob(t, repo, id).Status; got != models.JobCompleted {
		t.Fatalf("job is %q, want completed", got)
	}
}

func TestARunningJobKeepsItsLeasePastItsLength(t *testing.T) {
	svc, repo := jobServiceWithLocker(t, &countingLocker{})
	svc.settings.Lease = time.Second
	svc.settings.RenewEvery = 50 * time.Millisecond
	id := seedPendingJob(t, repo)
	claimFor(t, repo, id, time.Second, svc.workerID)

	runCtx, stopRun := context.WithCancelCause(t.Context())
	defer stopRun(nil)
	stop := svc.holdLease(runCtx, id, stopRun)
	time.Sleep(1500 * time.Millisecond)
	stop()

	if expires := *readJob(t, repo, id).LockExpires; !expires.After(time.Now()) {
		t.Fatalf("lease ran out at %s while the job was still running", expires)
	}
	if runCtx.Err() != nil {
		t.Fatalf("the run was stopped although its lease was held: %v", context.Cause(runCtx))
	}
	if claimed, _ := repo.Job().Lock(t.Context(), id, time.Minute, "rival-worker"); claimed {
		t.Fatal("another worker claimed a job whose lease was being renewed")
	}
}

func TestARunIsStoppedWhenItsLeaseIsLost(t *testing.T) {
	svc, repo := jobServiceWithLocker(t, &countingLocker{})
	svc.settings.Lease = time.Minute
	svc.settings.RenewEvery = 20 * time.Millisecond
	id := seedPendingJob(t, repo)
	claimFor(t, repo, id, time.Millisecond, svc.workerID)
	time.Sleep(20 * time.Millisecond)
	claimFor(t, repo, id, time.Minute, "rival-worker")

	runCtx, stopRun := context.WithCancelCause(t.Context())
	defer stopRun(nil)
	stop := svc.holdLease(runCtx, id, stopRun)
	defer stop()

	select {
	case <-runCtx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the run went on after its lease was lost")
	}
	if cause := context.Cause(runCtx); !errors.Is(cause, repocontracts.ErrJobLeaseLost) {
		t.Fatalf("run stopped with %v, want ErrJobLeaseLost", cause)
	}
}

// The outcomes that do not complete a job are fenced too: a late worker's
// retry or failure must not put a job the rival is running back in the queue.
func TestAWorkerThatLostItsLeaseCannotRequeueTheJob(t *testing.T) {
	svc, repo := jobServiceWithLocker(t, &countingLocker{})
	id := seedPendingJob(t, repo)
	claimFor(t, repo, id, time.Millisecond, svc.workerID)
	job := adapters.JobEntityAdapter{Model: readJob(t, repo, id)}.ToEntity()
	time.Sleep(20 * time.Millisecond)
	claimFor(t, repo, id, time.Minute, "rival-worker")

	job.Status = entities.JobPending
	svc.writeDetached(t.Context(), job)

	if after := readJob(t, repo, id); after.Status != models.JobRunning || after.LockedBy != "rival-worker" {
		t.Fatalf("job is %q held by %q; the late worker put the rival's job back", after.Status, after.LockedBy)
	}
}
