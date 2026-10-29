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
	definition, instance := caseIn(t.Context(), t, repo, project)
	if err := repo.ExternalTask().Create(entities.WithSystemContext(t.Context()), &models.ExternalTaskModel{
		Base:                models.Base{ID: models.UUID(uuid.New())},
		ProjectID:           models.UUID(project),
		ProcessInstanceID:   models.UUID(instance),
		ProcessDefinitionID: models.UUID(definition),
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
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
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
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
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
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
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

// A topic counts its tasks on offer — no lock, or one run out, and tries left —
// and its tasks under a live lock, each under the worker holding it, listed
// even when that worker was never recorded. A task with no tries left is
// neither.
func TestATopicCountsWhatIsOnOfferAndWhatEachWorkerHolds(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	ctx, _, project := testutils.ScopedProject(t, repo)
	now := time.Now().UTC()
	definition, instance := caseIn(ctx, t, repo, project)
	park := func(worker string, lock time.Duration, retries int) {
		t.Helper()
		m := &models.ExternalTaskModel{
			Base: models.Base{ID: models.UUID(uuid.New())}, ProjectID: models.UUID(project),
			ProcessInstanceID: models.UUID(instance), ProcessDefinitionID: models.UUID(definition),
			NodeID: "ship", Topic: "shipping", Retries: retries,
		}
		if worker != "" {
			w, until := worker, now.Add(lock)
			m.WorkerID, m.LockExpiration = w, &until
		}
		if err := repo.ExternalTask().Create(entities.WithSystemContext(t.Context()), m); err != nil {
			t.Fatalf("park a task: %v", err)
		}
	}
	park("", 0, 3)                 // on offer
	park("gone", -time.Minute, 3)  // its lock ran out: on offer
	park("packer-1", time.Hour, 3) // held
	park("packer-1", time.Hour, 3) // held
	park("loader-9", time.Hour, 1) // held, by a worker never recorded
	park("", 0, 0)                 // no tries left
	if err := repo.ExternalTaskWorker().Seen(ctx, "shipping", "packer-1", "", now); err != nil {
		t.Fatalf("record a worker: %v", err)
	}

	topics, err := repo.ExternalTaskWorker().Topics(ctx, now)
	if err != nil {
		t.Fatalf("list the topics: %v", err)
	}
	if len(topics) != 1 || topics[0].Waiting != 2 || topics[0].Locked != 3 {
		t.Fatalf("topics are %+v; want shipping with 2 on offer and 3 held", topics)
	}
	held := map[string]int64{}
	for _, w := range topics[0].Workers {
		held[w.WorkerID] = w.Locked
	}
	if len(held) != 2 || held["packer-1"] != 2 || held["loader-9"] != 1 {
		t.Errorf("workers hold %v; want packer-1 two and loader-9 one", held)
	}
}
