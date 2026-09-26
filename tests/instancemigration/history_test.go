package instancemigration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// A version's history is not its running work.
//
// A migration reads every instance of the version it moves off, and every job
// of each instance it moves. Both reads went through queries the store caps at
// a thousand rows — storm starts every query with that limit — so a version or
// an instance with more history than that was moved in part, and nothing said
// so: the run reported success.

// storeReadCap is the store's default limit on one read.
const storeReadCap = 1000

// historyHarness is a real facade over PostgreSQL, plus the repository, so a
// test can lay down the history a busy version gathers without running it.
type historyHarness struct {
	svc       services.ServiceFacade
	repo      repositories.Repository
	ctx       context.Context
	projectID uuid.UUID
}

func newHistoryHarness(t *testing.T) *historyHarness {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), observersimpl.NewSSEObserver(),
		"migration-history-test", nil, nil, nil)
	ctx, _, projectID := testutils.ScopedProject(t, repo)
	return &historyHarness{svc: svc, repo: repo, ctx: ctx, projectID: projectID}
}

func (h *historyHarness) deploy(t *testing.T, def *entities.ProcessDefinition) uuid.UUID {
	t.Helper()
	id, err := h.svc.CreateDefinition(h.ctx, def)
	if err != nil {
		t.Fatalf("deploy %s: %v", def.Key, err)
	}
	return id
}

// finishedSince records n instances of a version that ran to their end after
// everything started so far — newer, so a newest-first read meets them first.
func (h *historyHarness) finishedSince(t *testing.T, definitionID uuid.UUID, n int) {
	t.Helper()
	for range n {
		if _, err := h.repo.Process().Create(h.ctx, models.ProcessInstanceModel{
			ProjectID:    models.UUID(h.projectID),
			DefinitionID: models.UUID(definitionID),
			Status:       models.ProcessCompleted,
		}); err != nil {
			t.Fatalf("record a finished instance: %v", err)
		}
	}
}

// An instance started before a thousand others finished was not among the
// thousand newest the migration read, so it stayed on the version being
// retired — and running the migration again could not reach it either, because
// the finished instances stay on that version and keep filling the window.
func TestAnInstanceOlderThanAThousandFinishedOnesIsStillMigrated(t *testing.T) {
	h := newHistoryHarness(t)
	v1 := h.deploy(t, approval(h.projectID, "approve"))
	running, err := h.svc.StartProcess(h.ctx, h.projectID, "expense-approval", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	h.finishedSince(t, v1, storeReadCap)
	v2 := h.deploy(t, approval(h.projectID, "review"))

	if err := h.svc.MigrateInstances(h.ctx, v1, v2, map[string]string{"approve": "review"}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	instance, err := h.svc.GetInstance(h.ctx, running)
	if err != nil {
		t.Fatalf("read the running instance: %v", err)
	}
	if instance.Definition == nil || instance.Definition.ID != v2 {
		t.Fatalf("the migration reported success and the instance started before %d finished ones is still on version 1",
			storeReadCap)
	}
	if len(instance.GetTokensByNode(&entities.Node{ID: "review"})) != 1 {
		t.Fatalf("the migrated instance is not waiting on review: tokens %+v", instance.Tokens)
	}
}

// An instance's jobs include the ones that already ran — a repeating timer
// leaves one per occurrence. Behind a thousand of those, the one still waiting
// kept naming the old version and node, and when it came due it looked for
// that node in the old graph, found the instance was not there, and dismissed
// itself as stale. The migrated instance waited on it for good.
func TestATimerBehindAThousandJobsThatRanStillMovesWithItsInstance(t *testing.T) {
	h := newHistoryHarness(t)
	v1 := h.deploy(t, waitThenWork(h.projectID, "wait", "oldWork"))
	instanceID, err := h.svc.StartProcess(h.ctx, h.projectID, "cooling-off", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	jobs, err := h.repo.Job().ListByInstance(h.ctx, instanceID)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("expected the timer's one job: jobs=%d err=%v", len(jobs), err)
	}
	waiting := jobs[0]

	// Jobs that already ran on the same node, due before the one still waiting.
	for i := range storeReadCap {
		if _, err := h.repo.Job().Create(h.ctx, models.JobModel{
			InstanceID:   models.UUID(instanceID),
			DefinitionID: models.UUID(v1),
			NodeID:       "wait",
			Type:         models.JobTimer,
			Status:       models.JobCompleted,
			NextRunAt:    time.Now().Add(-time.Duration(i+1) * time.Minute),
		}); err != nil {
			t.Fatalf("record a job that ran: %v", err)
		}
	}
	v2 := h.deploy(t, waitThenWork(h.projectID, "pause", "newWork"))

	if err := h.svc.MigrateInstances(h.ctx, v1, v2, map[string]string{"wait": "pause", "oldWork": "newWork"}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	moved, err := h.repo.Job().Get(h.ctx, uuid.UUID(waiting.ID))
	if err != nil {
		t.Fatalf("read the waiting timer: %v", err)
	}
	if uuid.UUID(moved.DefinitionID) != v2 || moved.NodeID != "pause" {
		t.Fatalf("the migration reported success and the waiting timer still names version %s and node %q",
			moved.DefinitionID, moved.NodeID)
	}

	// And it fires where the instance now is.
	moved.NextRunAt = time.Now().Add(-time.Minute)
	if err := h.repo.Job().Update(h.ctx, moved); err != nil {
		t.Fatalf("bring the timer due: %v", err)
	}
	if err := h.svc.ProcessPendingJobs(h.ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}
	instance, err := h.svc.GetInstance(h.ctx, instanceID)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	if len(instance.GetTokensByNode(&entities.Node{ID: "newWork"})) != 1 {
		t.Fatalf("the timer did not carry the instance on to the new version's next step; tokens: %+v", instance.Tokens)
	}
}
