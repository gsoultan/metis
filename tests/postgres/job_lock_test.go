package postgres_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// A pending job is claimed without counting an attempt; a held lease is not
// taken; an expired one is, and that claim counts the attempt the dead worker
// never reported.
func TestTheLockCountsOnlyATakeover(t *testing.T) {
	repo := repositories.NewRepository(testutils.StormConn(testutils.SetupTestDB(t)))
	ctx := entities.WithSystemContext(t.Context())
	jobs := repo.Job()
	id, err := jobs.Create(ctx, models.JobModel{
		Base: models.Base{ID: models.UUID(uuid.New())},
		Type: "timer", Status: models.JobPending, NextRunAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("queue a job: %v", err)
	}
	attempt := func(worker string, lease time.Duration, want bool, retries int) {
		t.Helper()
		got, err := jobs.Lock(ctx, id, lease, worker)
		if err != nil || got != want {
			t.Fatalf("%s locked %v (%v), want %v", worker, got, err, want)
		}
		j, err := jobs.Get(ctx, id)
		if err != nil {
			t.Fatalf("read the job: %v", err)
		}
		if j.Status != models.JobRunning || j.Retries != retries {
			t.Errorf("after %s the job is %s with %d retries, want running with %d", worker, j.Status, j.Retries, retries)
		}
	}
	// The first lease is already over, so the next worker may take it.
	attempt("a", -time.Minute, true, 0)
	attempt("b", time.Hour, true, 1)
	attempt("c", time.Hour, false, 1)
}
