package postgres_test

import (
	"context"
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

// waitingOn parks a task on topic in project, on offer to any worker.
func waitingOn(t *testing.T, repo repositories.Repository, project uuid.UUID, topic string) {
	t.Helper()
	if err := repo.ExternalTask().Create(entities.WithSystemContext(t.Context()), &models.ExternalTaskModel{
		Base:                models.Base{ID: models.UUID(uuid.New())},
		ProjectID:           models.UUID(project),
		ProcessInstanceID:   models.UUID(uuid.New()),
		ProcessDefinitionID: models.UUID(uuid.New()),
		NodeID:              "ship",
		Topic:               topic,
		Retries:             3,
	}); err != nil {
		t.Fatalf("park a task: %v", err)
	}
}

func workerOn(t *testing.T, repo repositories.Repository, ctx context.Context, topic, workerID string) entities.ExternalTaskWorker {
	t.Helper()
	topics, err := repo.ExternalTaskWorker().Topics(ctx, time.Now())
	if err != nil {
		t.Fatalf("list the topics: %v", err)
	}
	for _, listed := range topics {
		for _, worker := range listed.Workers {
			if listed.Topic == topic && worker.WorkerID == workerID {
				return worker
			}
		}
	}
	t.Fatalf("%s is not listed on %s: %+v", workerID, topic, topics)
	return entities.ExternalTaskWorker{}
}

// A worker long-polling every second must not write a row a second. The row
// is written again once it is older than the throttle, and at once when the
// worker says it is another version — a deploy shows on the next fetch.
func TestAWorkerIsRecordedAtMostEveryTenSecondsUnlessItsVersionChanges(t *testing.T) {
	repo := repositories.NewRepository(testutils.StormConn(testutils.SetupTestDB(t)))
	ctx, _, _ := testutils.ScopedProject(t, repo)
	first := time.Now().Add(-time.Minute).UTC().Truncate(time.Microsecond)

	seen := func(at time.Time, version string) time.Time {
		t.Helper()
		if err := repo.ExternalTaskWorker().Seen(ctx, "shipping", "packer-1", version, at); err != nil {
			t.Fatalf("record the worker: %v", err)
		}
		worker := workerOn(t, repo, ctx, "shipping", "packer-1")
		if worker.Version != version {
			t.Fatalf("the worker is listed as version %q, want %q", worker.Version, version)
		}
		return *worker.LastSeenAt
	}

	if got := seen(first, "1.0.0"); !got.Equal(first) {
		t.Fatalf("first seen at %v, want %v", got, first)
	}
	if got := seen(first.Add(5*time.Second), "1.0.0"); !got.Equal(first) {
		t.Errorf("a fetch five seconds later wrote the row: last seen %v, want %v", got, first)
	}
	if got := seen(first.Add(6*time.Second), "1.1.0"); !got.Equal(first.Add(6 * time.Second)) {
		t.Errorf("a new version was not written at once: last seen %v", got)
	}
	if got := seen(first.Add(20*time.Second), "1.1.0"); !got.Equal(first.Add(20 * time.Second)) {
		t.Errorf("a fetch past the throttle was not written: last seen %v", got)
	}
}

// The alert counts a topic per organization: a worker serving one
// organization's topic serves nobody else's of the same name, and one that has
// not fetched for longer than the live window serves nothing.
func TestATopicIsUnservedWhenNoLiveWorkerOfItsOrganizationFetchesIt(t *testing.T) {
	repo := repositories.NewRepository(testutils.StormConn(testutils.SetupTestDB(t)))
	served, _, servedProject := testutils.ScopedProject(t, repo)
	alone, _, aloneProject := testutils.ScopedProject(t, repo)
	stale, _, staleProject := testutils.ScopedProject(t, repo)
	now := time.Now()

	waitingOn(t, repo, servedProject, "shipping")
	waitingOn(t, repo, aloneProject, "shipping")
	waitingOn(t, repo, staleProject, "shipping")
	for _, record := range []struct {
		ctx context.Context
		at  time.Time
	}{{served, now}, {stale, now.Add(-entities.WorkerLiveWindow - time.Minute)}} {
		if err := repo.ExternalTaskWorker().Seen(record.ctx, "shipping", "packer-1", "", record.at); err != nil {
			t.Fatalf("record a worker: %v", err)
		}
	}

	unserved, err := repo.ExternalTaskWorker().CountUnserved(entities.WithSystemContext(t.Context()), now)
	if err != nil {
		t.Fatalf("count the unserved topics: %v", err)
	}
	if unserved != 2 {
		t.Errorf("%d topics are unserved, want two: the organization with no worker and the one whose worker stopped", unserved)
	}

	for name, want := range map[string]struct {
		ctx      context.Context
		unserved bool
		workers  int
	}{
		"served": {served, false, 1}, "alone": {alone, true, 0}, "stale": {stale, true, 1},
	} {
		topics, err := repo.ExternalTaskWorker().Topics(want.ctx, now)
		if err != nil {
			t.Fatalf("list %s's topics: %v", name, err)
		}
		if len(topics) != 1 || topics[0].Waiting != 1 || topics[0].Unserved != want.unserved || len(topics[0].Workers) != want.workers {
			t.Errorf("%s's topics are %+v; want one task waiting on shipping, unserved %v, %d worker(s)", name, topics, want.unserved, want.workers)
		}
	}
}

// Workers not heard from for a week are forgotten, and neither the count nor
// the forgetting is a tenant's to run: both span every organization.
func TestIdleWorkersAreForgottenAndOnlyBySystemWork(t *testing.T) {
	repo := repositories.NewRepository(testutils.StormConn(testutils.SetupTestDB(t)))
	ctx, _, _ := testutils.ScopedProject(t, repo)
	now := time.Now()

	for workerID, at := range map[string]time.Time{"gone": now.Add(-8 * 24 * time.Hour), "here": now} {
		if err := repo.ExternalTaskWorker().Seen(ctx, "shipping", workerID, "", at); err != nil {
			t.Fatalf("record %s: %v", workerID, err)
		}
	}

	if _, err := repo.ExternalTaskWorker().ForgetIdle(ctx, now); !errors.Is(err, apierr.ErrForbidden) {
		t.Errorf("a tenant forgot every tenant's workers: %v", err)
	}
	if _, err := repo.ExternalTaskWorker().CountUnserved(ctx, now); !errors.Is(err, apierr.ErrForbidden) {
		t.Errorf("a tenant counted every tenant's topics: %v", err)
	}

	removed, err := repo.ExternalTaskWorker().ForgetIdle(entities.WithSystemContext(t.Context()), now.Add(-entities.WorkerForgottenAfter))
	if err != nil {
		t.Fatalf("forget idle workers: %v", err)
	}
	if removed != 1 {
		t.Errorf("forgot %d workers, want the one idle for eight days", removed)
	}
	workerOn(t, repo, ctx, "shipping", "here")
}
