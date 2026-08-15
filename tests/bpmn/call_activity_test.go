package bpmn_test

import (
	"strings"
	"testing"

	"github.com/gsoultan/gobpm/server/domains/entities"
)

// One process calling another.
//
// A call activity is how a process reuses another — "check the supplier" is a
// process in its own right, and three other processes call it. The engine has
// one and nothing ran one.

// The child process the tests below call.
func createChildProcess(t *testing.T, h *serviceTaskHarness, key string) {
	t.Helper()
	if _, err := h.defSvc.CreateDefinition(t.Context(), &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projectID},
		Key:     key,
		Name:    "Check the supplier",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: "review", Type: entities.UserTask, Name: "Compliance review", Assignee: "carol"},
			{ID: "end", Type: entities.EndEvent, Name: "Done"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "cf1", SourceRef: "start", TargetRef: "review"},
			{ID: "cf2", SourceRef: "review", TargetRef: "end"},
		},
	}); err != nil {
		t.Fatalf("create the child process: %v", err)
	}
}

// The name the designer writes has to be the name the engine reads.
func TestCallActivity_StartsTheProcessTheDesignerNamed(t *testing.T) {
	h := newServiceTaskHarness(t)
	createChildProcess(t, h, "supplier-check")

	parent := h.runDefinition(t, &entities.ProcessDefinition{
		Key:  "onboarding",
		Name: "Onboard a supplier",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: "check", Type: entities.CallActivity, Name: "Check the supplier", Properties: map[string]any{
				// What the property panel writes.
				"called_process_key": "supplier-check",
			}},
			{ID: "end", Type: entities.EndEvent, Name: "Done"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "check"},
			{ID: "f2", SourceRef: "check", TargetRef: "end"},
		},
	}, map[string]any{"supplierName": "Northwind"})

	// The child should be running, and waiting on its own task.
	tasks := h.tasksFor(t, parent.ID)
	children := h.subProcessesOf(t, parent.ID)
	if len(children) != 1 {
		t.Fatalf("the call activity started %d sub-processes — it could not tell which process to call", len(children))
	}
	if len(tasks) != 0 {
		t.Errorf("the parent has tasks of its own: %v", taskNames(tasks))
	}
	if childTasks := h.tasksFor(t, children[0].ID); len(childTasks) != 1 {
		t.Errorf("the child process is not waiting on its review task: %v", taskNames(childTasks))
	}
}

// The parent waits, and picks up again when the child is finished.
func TestCallActivity_ParentResumesWhenTheChildFinishes(t *testing.T) {
	h := newServiceTaskHarness(t)
	createChildProcess(t, h, "supplier-check")

	parent := h.runDefinition(t, &entities.ProcessDefinition{
		Key:  "onboarding-resume",
		Name: "Onboard a supplier",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: "check", Type: entities.CallActivity, Name: "Check the supplier", Properties: map[string]any{
				"called_process_key": "supplier-check",
			}},
			{ID: "sign", Type: entities.UserTask, Name: "Sign the contract", Assignee: "carol"},
			{ID: "end", Type: entities.EndEvent, Name: "Done"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "check"},
			{ID: "f2", SourceRef: "check", TargetRef: "sign"},
			{ID: "f3", SourceRef: "sign", TargetRef: "end"},
		},
	}, map[string]any{"supplierName": "Northwind"})

	children := h.subProcessesOf(t, parent.ID)
	if len(children) != 1 {
		t.Fatalf("no sub-process was started")
	}

	// Finish the child's task, which finishes the child.
	childTasks := h.tasksFor(t, children[0].ID)
	if len(childTasks) != 1 {
		t.Fatalf("the child is not waiting on a task: %v", taskNames(childTasks))
	}
	if err := h.taskSvc.CompleteTask(t.Context(), childTasks[0].ID, "carol", map[string]any{"approved": true}); err != nil {
		t.Fatalf("complete the child's task: %v", err)
	}

	// The parent should have moved on to its next step.
	parentTasks := h.tasksFor(t, parent.ID)
	if len(parentTasks) != 1 || parentTasks[0].Name != "Sign the contract" {
		t.Errorf("the parent did not carry on after the child finished; its tasks are %v", taskNames(parentTasks))
	}
}

// A call activity naming a process that does not exist is a modelling mistake.
// The engine reports it when the process runs rather than starting something
// that then waits for a child that will never exist.
func TestCallActivity_ReportsAProcessThatDoesNotExist(t *testing.T) {
	h := newServiceTaskHarness(t)
	ctx := t.Context()

	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projectID},
		Key:     "onboarding-missing",
		Name:    "Onboard a supplier",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: "check", Type: entities.CallActivity, Name: "Check the supplier", Properties: map[string]any{
				"called_process_key": "no-such-process",
			}},
			{ID: "end", Type: entities.EndEvent, Name: "Done"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "check"},
			{ID: "f2", SourceRef: "check", TargetRef: "end"},
		},
	}
	if _, err := h.defSvc.CreateDefinition(ctx, def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	_, err := h.engine.StartProcess(ctx, h.projectID, def.Key, map[string]any{})
	if err == nil {
		t.Fatal("a call activity naming a process that does not exist was allowed to run")
	}
	if !strings.Contains(err.Error(), "definition") {
		t.Errorf("the error does not say what could not be found: %v", err)
	}
}

// A call activity with nothing filled in says so, rather than starting nothing
// and leaving the process waiting for a child it never asked for.
func TestCallActivity_ReportsWhenNoProcessIsNamed(t *testing.T) {
	h := newServiceTaskHarness(t)
	ctx := t.Context()

	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projectID},
		Key:     "onboarding-unset",
		Name:    "Onboard a supplier",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: "check", Type: entities.CallActivity, Name: "Check the supplier"},
			{ID: "end", Type: entities.EndEvent, Name: "Done"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "check"},
			{ID: "f2", SourceRef: "check", TargetRef: "end"},
		},
	}
	if _, err := h.defSvc.CreateDefinition(ctx, def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	_, err := h.engine.StartProcess(ctx, h.projectID, def.Key, map[string]any{})
	if err == nil {
		t.Fatal("a call activity with no process named was allowed to run")
	}
	if !strings.Contains(err.Error(), "which process to call") {
		t.Errorf("the error does not say what is missing: %v", err)
	}
}
