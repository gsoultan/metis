package postgres_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// The metrics endpoint's view of the engine: what is due and waiting, how long
// the oldest has waited, which leases ran out, and what is broken. Each count
// has a case that must not be in it.
func TestTheBacklogCountsWhatIsWaitingAndNothingElse(t *testing.T) {
	repo := repositories.NewRepository(testutils.StormConn(testutils.SetupTestDB(t)))
	ctx := entities.WithSystemContext(t.Context())
	now := time.Now().UTC()
	oldest := now.Add(-10 * time.Minute)
	expired, live := now.Add(-time.Minute), now.Add(time.Minute)

	for _, j := range []models.JobModel{
		{Status: models.JobPending, NextRunAt: oldest},                                          // due
		{Status: models.JobPending, NextRunAt: now.Add(-time.Minute)},                           // due
		{Status: models.JobPending, NextRunAt: now.Add(time.Hour)},                              // not yet
		{Status: models.JobRunning, NextRunAt: oldest, LockExpires: &expired, LockedBy: "gone"}, // lease ran out
		{Status: models.JobRunning, NextRunAt: oldest, LockExpires: &live, LockedBy: "working"}, // being worked
		{Status: models.JobCompleted, NextRunAt: oldest},                                        // done
	} {
		j.ID = models.UUID(uuid.New())
		j.Type = "timer"
		if _, err := repo.Job().Create(ctx, j); err != nil {
			t.Fatalf("seed a job: %v", err)
		}
	}
	for _, status := range []models.IncidentStatus{models.IncidentOpen, models.IncidentOpen, models.IncidentResolved} {
		if _, err := repo.Incident().Create(ctx, models.IncidentModel{
			Base: models.Base{ID: models.UUID(uuid.New())}, Status: status, Error: "a service task failed",
		}); err != nil {
			t.Fatalf("seed an incident: %v", err)
		}
	}

	backlog, err := repo.Job().Backlog(ctx, now)
	if err != nil {
		t.Fatalf("backlog: %v", err)
	}
	if backlog.Due != 2 || backlog.LeaseExpired != 1 {
		t.Errorf("due=%d leaseExpired=%d, want 2 and 1", backlog.Due, backlog.LeaseExpired)
	}
	if !backlog.OldestDue.Equal(oldest.Truncate(time.Microsecond)) {
		t.Errorf("oldest due = %v, want %v", backlog.OldestDue, oldest)
	}
	open, err := repo.Incident().CountOpen(ctx)
	if err != nil || open != 2 {
		t.Errorf("open incidents = %d (err %v), want 2", open, err)
	}
}

// Both count across every tenant, so neither answers a tenant.
func TestTheBacklogIsNotATenantsToRead(t *testing.T) {
	repo := repositories.NewRepository(testutils.StormConn(testutils.SetupTestDB(t)))
	ctx, _ := testutils.ScopedContext(t, repo)

	if _, err := repo.Job().Backlog(ctx, time.Now()); !errors.Is(err, apierr.ErrForbidden) {
		t.Errorf("a tenant read the job backlog: %v", err)
	}
	if _, err := repo.Incident().CountOpen(ctx); !errors.Is(err, apierr.ErrForbidden) {
		t.Errorf("a tenant counted every tenant's incidents: %v", err)
	}
}
