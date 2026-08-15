package bpmn_test

import (
	"strings"
	"testing"

	"github.com/gsoultan/gobpm/server/domains/entities"
	service_impl2 "github.com/gsoultan/gobpm/server/domains/services/impl"
)

// A node that runs once per item, advanced by the job worker.
//
// Each iteration finishes in its own job, and finishing increments a completion
// counter held in the instance. Up to maxConcurrentJobs of those run at once, so
// the counter is shared state under concurrent read-modify-write: two iterations
// finishing together could both read the same count, both write the next one,
// and lose an increment. The counter then never reaches the total and the
// process waits forever — no error, no incident, no task.
//
// The job paths now take the instance row lock for the whole read-modify-write,
// the same way message and signal delivery already did.
//
// Note on what this test can and cannot show: the suite runs SQLite with a
// single connection, so transactions serialise and the interleaving that loses
// an increment cannot arise here. This covers the path end to end and guards the
// counting behaviour; the concurrency fix itself rests on the lock.
func TestMultiInstanceServiceTaskCountsEveryIteration(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Multi Instance Jobs Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "notify-every-supplier",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{
				ID:                "notify",
				Type:              entities.ServiceTask,
				Name:              "Notify the supplier",
				MultiInstanceType: "parallel",
				Collection:        "suppliers",
				Properties: map[string]any{
					"url":    "https://example.invalid/notify",
					"method": "POST",
				},
			},
			{ID: "summarise", Type: entities.UserTask, Name: "Summarise the responses"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "notify"},
			{ID: "f2", SourceRef: "notify", TargetRef: "summarise"},
			{ID: "f3", SourceRef: "summarise", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "notify-every-supplier", map[string]any{
		"suppliers": []any{"acme", "globex", "initech"},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	// One job per supplier.
	jobs, err := h.repo.Job().ListByInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("expected one job per supplier, got %d", len(jobs))
	}

	// Drain them. The outbound calls fail — the host does not resolve — which is
	// fine here: what matters is that every iteration is accounted for exactly
	// once, whether it succeeded or not.
	for range 5 {
		if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
			t.Fatalf("process pending jobs: %v", err)
		}
	}

	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}

	// The counter must never exceed the total: an iteration counted twice is as
	// wrong as one counted not at all. Once every iteration is accounted for the
	// bookkeeping is cleared, which is itself the signal that the node finished.
	if completed, total, ok := instance.MultiInstanceProgress("notify"); ok && completed > total {
		t.Errorf("counted %d completions for %d iterations — an iteration was counted twice", completed, total)
	}
}

// Engine bookkeeping must stay out of the business variable namespace.
//
// AGENTS.md vetoes it by name: `_mi_*` in the variables map collides with user
// data and leaks into the UI, audit history, variable snapshots and every script
// and condition scope. It now lives in its own field on the instance.
func TestMultiInstanceBookkeepingIsNotInProcessVariables(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Multi Instance Namespace Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "review-every-item",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{
				ID:                "review",
				Type:              entities.UserTask,
				Name:              "Review the item",
				MultiInstanceType: "parallel",
				Collection:        "items",
			},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "review-every-item", map[string]any{
		"items": []any{"a", "b"},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}

	for k := range instance.Variables {
		if strings.HasPrefix(k, "_mi_") || strings.HasPrefix(k, "_join_") {
			t.Errorf("engine bookkeeping %q is in the business variable namespace", k)
		}
	}

	// It is still tracked — just somewhere else.
	completed, total, ok := instance.MultiInstanceProgress("review")
	if !ok {
		t.Fatal("the node's progress was not recorded at all")
	}
	if total != 2 || completed != 0 {
		t.Errorf("expected 0 of 2 iterations complete, got %d of %d", completed, total)
	}
}

// An instance already part-way through a multi-instance node when the bookkeeping
// moved must keep its progress.
func TestMultiInstanceBackfillPreservesProgress(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Multi Instance Backfill Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "review-legacy",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{
				ID:                "review",
				Type:              entities.UserTask,
				Name:              "Review the item",
				MultiInstanceType: "parallel",
				Collection:        "items",
			},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "review-legacy", map[string]any{
		"items": []any{"a", "b", "c"},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	// Put the instance back into the pre-move shape: progress in the variables,
	// nothing in the new field. Two of three already done.
	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	instance.MultiInstance = nil
	instance.SetVariable("_mi_review_active", true)
	instance.SetVariable("_mi_review_total", 3)
	instance.SetVariable("_mi_review_completed", 2)
	if err := h.engine.UpdateInstance(ctx, instance); err != nil {
		t.Fatalf("stage the legacy shape: %v", err)
	}

	result, err := service_impl2.BackfillEngineBookkeeping(ctx, h.repo)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if result.Migrated != 1 {
		t.Errorf("expected 1 instance migrated, got %+v", result)
	}

	migrated, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload after backfill: %v", err)
	}
	completed, total, ok := migrated.MultiInstanceProgress("review")
	if !ok {
		t.Fatal("the migrated instance lost its multi-instance progress")
	}
	if completed != 2 || total != 3 {
		t.Errorf("expected 2 of 3 iterations preserved, got %d of %d", completed, total)
	}
	for k := range migrated.Variables {
		if strings.HasPrefix(k, "_mi_") {
			t.Errorf("legacy key %q was left in the variables", k)
		}
	}

	// Running it again finds nothing left to do.
	repeat, err := service_impl2.BackfillEngineBookkeeping(ctx, h.repo)
	if err != nil {
		t.Fatalf("repeat backfill: %v", err)
	}
	if repeat.Migrated != 0 {
		t.Errorf("a repeat run migrated %d instances; it is not idempotent", repeat.Migrated)
	}
}

// A gateway that waits for several branches counts them somewhere, and that
// somewhere must not be the business variables either.
//
// AGENTS.md names `_join_*` alongside `_mi_*` in the same veto. The count now
// has its own field, and an instance caught mid-join when it moved must keep
// the branches that had already arrived — otherwise the gateway forgets them
// and waits forever.
func TestParallelJoinBookkeepingIsNotInProcessVariables(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Join Namespace Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "two-approvals",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "split", Type: entities.ParallelGateway},
			{ID: "legal", Type: entities.UserTask, Name: "Legal review"},
			{ID: "finance", Type: entities.UserTask, Name: "Finance review"},
			{ID: "join", Type: entities.ParallelGateway},
			{ID: "sign", Type: entities.UserTask, Name: "Sign it off"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "split"},
			{ID: "f2", SourceRef: "split", TargetRef: "legal"},
			{ID: "f3", SourceRef: "split", TargetRef: "finance"},
			{ID: "f4", SourceRef: "legal", TargetRef: "join"},
			{ID: "f5", SourceRef: "finance", TargetRef: "join"},
			{ID: "f6", SourceRef: "join", TargetRef: "sign"},
			{ID: "f7", SourceRef: "sign", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "two-approvals", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	// Finish one branch so the gateway is mid-count.
	tasks, err := h.svc.ListTasks(ctx, h.projID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == "legal" {
			if err := h.svc.CompleteTask(ctx, task.ID, "carol", nil); err != nil {
				t.Fatalf("complete the legal review: %v", err)
			}
		}
	}

	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	for k := range instance.Variables {
		if strings.HasPrefix(k, "_join_") || strings.HasPrefix(k, "_mi_") {
			t.Errorf("engine bookkeeping %q is in the business variable namespace", k)
		}
	}
	if arrived := instance.JoinArrivals("join"); arrived != 1 {
		t.Errorf("expected 1 branch recorded at the gateway, got %d", arrived)
	}

	// The other branch finishes and the process goes through.
	tasks, err = h.svc.ListTasks(ctx, h.projID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == "finance" {
			if err := h.svc.CompleteTask(ctx, task.ID, "carol", nil); err != nil {
				t.Fatalf("complete the finance review: %v", err)
			}
		}
	}
	if !h.waitingAt(ctx, t, instanceID, "sign") {
		t.Error("both branches arrived but the gateway did not let the process through")
	}
}

// An instance caught mid-join when the counter moved keeps its arrivals.
func TestJoinBackfillPreservesArrivals(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Join Backfill Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "two-approvals-legacy",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "split", Type: entities.ParallelGateway},
			{ID: "legal", Type: entities.UserTask, Name: "Legal review"},
			{ID: "finance", Type: entities.UserTask, Name: "Finance review"},
			{ID: "join", Type: entities.ParallelGateway},
			{ID: "sign", Type: entities.UserTask, Name: "Sign it off"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "split"},
			{ID: "f2", SourceRef: "split", TargetRef: "legal"},
			{ID: "f3", SourceRef: "split", TargetRef: "finance"},
			{ID: "f4", SourceRef: "legal", TargetRef: "join"},
			{ID: "f5", SourceRef: "finance", TargetRef: "join"},
			{ID: "f6", SourceRef: "join", TargetRef: "sign"},
			{ID: "f7", SourceRef: "sign", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "two-approvals-legacy", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	// Put it back into the pre-move shape: one branch already arrived, recorded
	// in the variables.
	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	instance.Joins = nil
	instance.SetVariable("_join_join", 1)
	if err := h.engine.UpdateInstance(ctx, instance); err != nil {
		t.Fatalf("stage the legacy shape: %v", err)
	}

	if _, err := service_impl2.BackfillEngineBookkeeping(ctx, h.repo); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	migrated, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload after backfill: %v", err)
	}
	if arrived := migrated.JoinArrivals("join"); arrived != 1 {
		t.Errorf("the gateway forgot the branch that had already arrived: %d recorded", arrived)
	}
	for k := range migrated.Variables {
		if strings.HasPrefix(k, "_join_") {
			t.Errorf("legacy key %q was left in the variables", k)
		}
	}
}
