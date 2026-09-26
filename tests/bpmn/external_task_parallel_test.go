package bpmn_test

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// Completing an external task read the instance without holding it, set the
// worker's variables, and wrote the instance back whole. Two branches whose
// workers finished at the same moment each wrote a token list without the
// other's progress in it, so the join waited for a branch that had finished:
// the instance never completed, and nothing said why.
func TestTwoBranchesCompletedTogetherBothReachTheJoin(t *testing.T) {
	h := newEngineHarness(t, "External Parallel Project")
	ctx := h.Ctx()
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "check-both",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "fork", Type: entities.ParallelGateway},
			{ID: "credit", Type: entities.ServiceTask, Name: "Check credit", ExternalTopic: "credit"},
			{ID: "fraud", Type: entities.ServiceTask, Name: "Check fraud", ExternalTopic: "fraud"},
			{ID: "join", Type: entities.ParallelGateway},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "fork"},
			{ID: "f2", SourceRef: "fork", TargetRef: "credit"},
			{ID: "f3", SourceRef: "fork", TargetRef: "fraud"},
			{ID: "f4", SourceRef: "credit", TargetRef: "join"},
			{ID: "f5", SourceRef: "fraud", TargetRef: "join"},
			{ID: "f6", SourceRef: "join", TargetRef: "end"},
		},
	})

	const instances = 6
	var ids []uuid.UUID
	for range instances {
		id, err := h.svc.StartProcess(ctx, h.projID, "check-both", nil)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		ids = append(ids, id)
	}
	credit, err := h.svc.FetchAndLock(ctx, "credit", "worker", instances, 60_000)
	if err != nil || len(credit) != instances {
		t.Fatalf("fetch credit: %d, %v", len(credit), err)
	}
	fraud, err := h.svc.FetchAndLock(ctx, "fraud", "worker", instances, 60_000)
	if err != nil || len(fraud) != instances {
		t.Fatalf("fetch fraud: %d, %v", len(fraud), err)
	}

	// Both branches of every instance finish at once.
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, task := range append(credit, fraud...) {
		wg.Go(func() {
			<-start
			if err := h.svc.Complete(ctx, task.ID, "worker", map[string]any{task.Node.ID + "Checked": true}); err != nil {
				t.Errorf("complete %s: %v", task.Node.ID, err)
			}
		})
	}
	close(start)
	wg.Wait()

	stuck := 0
	for _, id := range ids {
		instance, err := h.svc.GetInstance(ctx, id)
		if err != nil {
			t.Fatalf("read instance: %v", err)
		}
		if instance.Status != entities.ProcessCompleted {
			stuck++
		}
	}
	if stuck > 0 {
		t.Fatalf("%d of %d instances whose branches both finished never completed", stuck, instances)
	}
}
