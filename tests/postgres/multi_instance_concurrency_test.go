package postgres_test

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/tests/testutils"
)

// Counting the iterations of a node that runs once per item, under real
// concurrency.
//
// Finishing an iteration increments a completion counter held in the instance,
// and the job worker runs several iterations at once. That makes the counter
// shared state under read-modify-write. If two workers read the same count and
// both write the next one, an increment is lost, the counter never reaches the
// total, and the process waits forever with no error and no incident.
//
// None of this is observable on SQLite with a single connection, which is what
// the rest of the suite runs: transactions serialise, so the interleaving cannot
// arise. It needs a database that can actually run two transactions at once.

func multiInstanceDefinition(projID uuid.UUID, key string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projID},
		Key:     key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{
				ID:                "review",
				Type:              entities.UserTask,
				Name:              "Review the item",
				MultiInstanceType: "parallel",
				Collection:        "items",
			},
			{ID: "summarise", Type: entities.UserTask, Name: "Summarise"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "summarise"},
			{ID: "f3", SourceRef: "summarise", TargetRef: "end"},
		},
	}
}

// An unlocked read-modify-write loses increments.
//
// This characterises the pattern rather than the current code: each worker reads
// the instance without taking the row lock, exactly as the job paths did before
// they were changed to GetInstanceForUpdate. The reads are held behind a barrier
// so the interleaving is certain rather than a matter of timing.
//
// It is the justification for the lock, and it is why the lock has to be taken
// for the whole read-modify-write rather than just the read.
func TestUnlockedInstanceReadLosesIterationCounts(t *testing.T) {
	db := testutils.SetupPostgresDB(t, 8)
	ctx := t.Context()
	repo, engine, projID := newPostgresEngine(t, db)

	defSvc := serviceimpl.NewDefinitionService(repo)
	if _, err := defSvc.CreateDefinition(ctx, multiInstanceDefinition(projID, "review-unlocked")); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	instanceID, err := engine.StartProcess(ctx, projID, "review-unlocked", map[string]any{
		"items": []any{"a", "b", "c", "d"},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	def, err := engine.GetProcessDefinition(ctx, mustInstanceDefID(t, engine, instanceID))
	if err != nil {
		t.Fatalf("load definition: %v", err)
	}

	// Every worker reads before any of them writes.
	const workers = 4
	var loaded, release sync.WaitGroup
	loaded.Add(workers)
	release.Add(1)

	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(iteration int) {
			defer wg.Done()
			snapshot, err := engine.GetInstance(ctx, instanceID)
			if err != nil {
				t.Errorf("worker %d load: %v", iteration, err)
				loaded.Done()
				return
			}
			loaded.Done()
			release.Wait()
			// Errors are not the point here; the counter is.
			_ = engine.ProceedIteration(ctx, &snapshot, def, "review", itoa(iteration))
		}(i)
	}

	loaded.Wait()
	release.Done()
	wg.Wait()

	final, err := engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	completed := asInt(final.Variables["_mi_review_completed"])

	if completed == workers {
		t.Skip("the unlocked reads happened to serialise on this run; nothing to characterise")
	}
	t.Logf("unlocked reads counted %d of %d iterations — the difference is what a lost increment costs", completed, workers)
	if len(final.GetTokensByNode(&entities.Node{ID: "summarise"})) != 0 {
		t.Error("the process advanced despite a lost increment; the characterisation no longer holds")
	}
	t.Log("the process is now stuck at the node forever: no error, no incident, no task")
}

// The job path takes the row lock, so every iteration is counted exactly once
// even when several finish at the same moment.
func TestMultiInstanceCountsEveryIterationUnderConcurrentJobs(t *testing.T) {
	db := testutils.SetupPostgresDB(t, 8)
	ctx := t.Context()
	repo, engine, projID := newPostgresEngine(t, db)

	defSvc := serviceimpl.NewDefinitionService(repo)
	if _, err := defSvc.CreateDefinition(ctx, multiInstanceDefinition(projID, "review-locked")); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	instanceID, err := engine.StartProcess(ctx, projID, "review-locked", map[string]any{
		"items": []any{"a", "b", "c", "d"},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	def, err := engine.GetProcessDefinition(ctx, mustInstanceDefID(t, engine, instanceID))
	if err != nil {
		t.Fatalf("load definition: %v", err)
	}

	// Same shape as the job worker: each worker takes the instance row lock for
	// the whole read-modify-write.
	const workers = 4
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(iteration int) {
			defer wg.Done()
			err := repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
				snapshot, err := engine.GetInstanceForUpdate(txCtx, instanceID)
				if err != nil {
					return err
				}
				return engine.ProceedIteration(txCtx, &snapshot, def, "review", itoa(iteration))
			})
			if err != nil {
				t.Errorf("worker %d: %v", iteration, err)
			}
		}(i)
	}
	wg.Wait()

	final, err := engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	// Every iteration counted means the node finished: the engine clears its
	// bookkeeping and puts a token on the next step. A lost increment leaves the
	// counter short, the bookkeeping in place and the token stuck here.
	if _, stillCounting := final.Variables["_mi_review_completed"]; stillCounting {
		t.Errorf("the node is still counting after all %d iterations finished (counter %v of %v) — an increment was lost",
			workers, final.Variables["_mi_review_completed"], final.Variables["_mi_review_total"])
	}
	if len(final.GetTokensByNode(&entities.Node{ID: "summarise"})) == 0 {
		t.Errorf("all %d iterations finished but the process never moved past the node", workers)
	}
}

func mustInstanceDefID(t *testing.T, engine *serviceimpl.Engine, instanceID uuid.UUID) uuid.UUID {
	t.Helper()
	instance, err := engine.GetInstance(t.Context(), instanceID)
	if err != nil {
		t.Fatalf("load instance: %v", err)
	}
	if instance.Definition == nil {
		t.Fatal("instance has no definition reference")
	}
	return instance.Definition.ID
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return -1
	}
}

func itoa(i int) string {
	return strconv.Itoa(i)
}
