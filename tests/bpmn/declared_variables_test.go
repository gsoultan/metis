package bpmn_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/tests/testutils"
)

// What a user task hands back to its process.
//
// BPMN 2.0.2 §10.4 (Items and Data): an activity's results reach the process
// through the data outputs its InputOutputSpecification declares, and nothing
// else it produces does. Metis does not model ioSpecification. A user task's
// form stands in for it: each field's id is a variable the task may set. That
// is the deviation, stated — and it is why the rule below is the spec's rule
// rather than a new one.
//
// The engine did not keep it. Completing a task wrote every variable the
// request carried into the instance, so the person approving a refund could
// rewrite the amount being refunded on the way past.

// refund is a process whose one step asks carol whether a refund is approved.
func refund(projectID uuid.UUID, step *entities.Node) *entities.ProcessDefinition {
	step.ID, step.Type, step.Name, step.Assignee = "approve", entities.UserTask, "Approve the refund", "carol"
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "refund",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			step,
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "end"},
		},
	}
}

// openRefund deploys refund with the step given, starts it for 120, and
// returns the instance and its task.
func openRefund(t *testing.T, h engineHarness, step *entities.Node) (uuid.UUID, uuid.UUID) {
	t.Helper()
	h.deploy(t, refund(h.projID, step))
	instanceID, err := h.svc.StartProcess(h.Ctx(), h.projID, "refund", map[string]any{"amount": 120})
	if err != nil {
		t.Fatalf("start the refund: %v", err)
	}
	tasks, err := h.svc.ListTasks(h.Ctx(), h.projID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && taskIsOpen(task.Status) {
			return instanceID, task.ID
		}
	}
	t.Fatal("the refund opened no task")
	return uuid.Nil, uuid.Nil
}

func TestAUserTaskHandsBackOnlyTheVariablesItsFormDeclares(t *testing.T) {
	h := newEngineHarness(t, "Declared Variables Project")
	ctx := h.Ctx()
	instanceID, taskID := openRefund(t, h, &entities.Node{Properties: testutils.FormDeclaring("approved")})

	err := h.svc.CompleteTask(ctx, taskID, "carol", map[string]any{"approved": true, "amount": 1200000})
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "no field named amount") {
		t.Fatalf("approving the refund and rewriting its amount: got %v, want a refusal naming amount", err)
	}

	// Nothing moved: the token waits at the step and the amount is the one the
	// refund was raised for.
	if !h.waitingAt(ctx, t, instanceID, "approve") {
		t.Fatal("after the refusal the instance is no longer waiting at the approval")
	}
	instance, err := h.svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	if fmt.Sprint(instance.Variables["amount"]) != "120" {
		t.Fatalf("after the refusal the amount is %v, want 120", instance.Variables["amount"])
	}
	if value, set := instance.Variables["approved"]; set {
		t.Fatalf("after the refusal the instance holds approved = %v, from a completion that did not happen", value)
	}

	// What the form declares is handed back, and the process moves on.
	if err := h.svc.CompleteTask(ctx, taskID, "carol", map[string]any{"approved": true}); err != nil {
		t.Fatalf("approving the refund with the form's field: %v", err)
	}
	instance, err = h.svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	if instance.Status != entities.ProcessCompleted || instance.Variables["approved"] != true ||
		fmt.Sprint(instance.Variables["amount"]) != "120" {
		t.Fatalf("after the approval the instance is %q holding %v; want completed, approved, and the amount unchanged",
			instance.Status, instance.Variables)
	}
}

// A user task with no form declares no outputs, so it hands back nothing — and
// still completes when nothing is handed back.
func TestAUserTaskWithNoFormHandsBackNothing(t *testing.T) {
	h := newEngineHarness(t, "No Form Project")
	ctx := h.Ctx()
	instanceID, taskID := openRefund(t, h, &entities.Node{})

	err := h.svc.CompleteTask(ctx, taskID, "carol", map[string]any{"approved": true})
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "no form to declare approved") {
		t.Fatalf("setting a variable on a task with no form: got %v, want a refusal saying it has no form", err)
	}
	if !h.waitingAt(ctx, t, instanceID, "approve") {
		t.Fatal("after the refusal the instance is no longer waiting at the step")
	}

	if err := h.svc.CompleteTask(ctx, taskID, "carol", nil); err != nil {
		t.Fatalf("completing a task with no form and no variables: %v", err)
	}
	instance, err := h.svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	if instance.Status != entities.ProcessCompleted {
		t.Fatalf("after the completion the instance is %q, want completed", instance.Status)
	}
}
