package bpmn_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/gobpm/server/domains/entities"
)

// An ad-hoc sub-process lets a knowledge worker run the tasks inside it in any
// order, any number of times, until a completion condition is satisfied.
//
// The engine models the waiting half of that but not the acting half: there is
// an AdHocActivator contract describing how a worker activates a task, and
// nothing in the repository implements or calls it. So an ad-hoc sub-process
// whose condition is not yet met has no route forward at all.
//
// A process that can never continue must say so. Parking a token and reporting
// success is the one outcome an orchestrator cannot afford: the instance is a
// durable business commitment, and a silent hang leaves no incident to
// investigate.

// completeTaskAt finishes the open task at nodeID, passing variables through.
func completeTaskAt(ctx context.Context, t *testing.T, h engineHarness, instanceID uuid.UUID, nodeID string, vars map[string]any) {
	t.Helper()

	tasks, err := h.svc.ListTasks(ctx, h.projID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == nodeID && taskIsOpen(task.Status) {
			if err := h.svc.CompleteTask(ctx, task.ID, "carol", vars); err != nil {
				t.Fatalf("complete %s: %v", nodeID, err)
			}
			return
		}
	}
	t.Fatalf("no open task at %s to complete", nodeID)
}

func adHocDefinition(key, completionCondition string) entities.ProcessDefinition {
	return entities.ProcessDefinition{
		Key: key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{
				ID:                  "research",
				Type:                entities.SubProcess,
				Name:                "Research the claim",
				IsAdHoc:             true,
				CompletionCondition: completionCondition,
				Nodes: []*entities.Node{
					{ID: "call-customer", Type: entities.UserTask, Name: "Call the customer", ParentID: "research"},
					{ID: "check-records", Type: entities.UserTask, Name: "Check the records", ParentID: "research"},
				},
			},
			{ID: "decide", Type: entities.UserTask, Name: "Decide the claim"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "research"},
			{ID: "f2", SourceRef: "research", TargetRef: "decide"},
			{ID: "f3", SourceRef: "decide", TargetRef: "end"},
		},
	}
}

// A knowledge worker runs the steps in whatever order the work needs, and the
// sub-process finishes when its completion condition says so.
func TestAdHocSubProcessRunsStepsOnDemand(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "AdHoc Activation Project")

	def := adHocDefinition("claim-research", "js:reviewsDone >= 2")
	def.Project = &entities.Project{ID: h.projID}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "claim-research", map[string]any{"reviewsDone": 0})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	// Waiting inside the sub-process, with nothing started yet.
	if h.waitingAt(ctx, t, instanceID, "decide") {
		t.Fatal("the process ran past the sub-process without doing any of the work")
	}
	if h.waitingAt(ctx, t, instanceID, "call-customer") {
		t.Fatal("a step started on its own; the point of ad-hoc is that it does not")
	}

	// The worker decides to call the customer first.
	if err := h.svc.ActivateTask(ctx, instanceID, "research", "call-customer"); err != nil {
		t.Fatalf("activate the call: %v", err)
	}
	if !h.waitingAt(ctx, t, instanceID, "call-customer") {
		t.Fatal("the activated step was not offered")
	}

	completeTaskAt(ctx, t, h, instanceID, "call-customer", map[string]any{"reviewsDone": 1})

	// One review done, condition not met, so the sub-process is still open.
	if h.waitingAt(ctx, t, instanceID, "decide") {
		t.Fatal("the sub-process finished before its completion condition was met")
	}

	// The same step can be run again — that is what ad-hoc means.
	if err := h.svc.ActivateTask(ctx, instanceID, "research", "call-customer"); err != nil {
		t.Fatalf("activate the call a second time: %v", err)
	}
	completeTaskAt(ctx, t, h, instanceID, "call-customer", map[string]any{"reviewsDone": 2})

	// The condition is satisfied, so the process moves on.
	if !h.waitingAt(ctx, t, instanceID, "decide") {
		t.Error("the completion condition was met but the process did not carry on")
	}
}

// Activation is checked against the process, not taken on trust.
func TestAdHocActivationRefusesWhatIsNotThere(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "AdHoc Refusal Project")

	def := adHocDefinition("claim-research-guard", "js:reviewsDone >= 2")
	def.Project = &entities.Project{ID: h.projID}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "claim-research-guard", map[string]any{"reviewsDone": 0})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	for _, tc := range []struct {
		name       string
		subProcess string
		task       string
		wantIn     string
	}{
		{name: "a step that is not inside it", subProcess: "research", task: "decide", wantIn: "not one of the steps"},
		{name: "a step that does not exist", subProcess: "research", task: "nonsense", wantIn: "not one of the steps"},
		{name: "a sub-process that does not exist", subProcess: "nonsense", task: "call-customer", wantIn: "no step called"},
		{name: "a step that is not ad-hoc", subProcess: "decide", task: "call-customer", wantIn: "not an ad-hoc sub-process"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := h.svc.ActivateTask(ctx, instanceID, tc.subProcess, tc.task)
			if err == nil {
				t.Fatal("the activation was accepted")
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("the error does not explain the refusal: %v", err)
			}
		})
	}
}

// A step cannot be started in a sub-process the process is not currently in.
func TestAdHocActivationRefusesWhenTheProcessIsElsewhere(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "AdHoc Elsewhere Project")

	def := adHocDefinition("claim-research-done-already", "")
	def.Project = &entities.Project{ID: h.projID}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	// No completion condition, so it passes straight through and the process is
	// past the sub-process by the time anyone asks.
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "claim-research-done-already", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	err = h.svc.ActivateTask(ctx, instanceID, "research", "call-customer")
	if err == nil {
		t.Fatal("a step was started in a sub-process the process had already left")
	}
	if !strings.Contains(err.Error(), "not currently inside") {
		t.Errorf("the error does not explain why: %v", err)
	}
}

// An ad-hoc sub-process with no completion condition has nothing to wait for and
// carries on. This path works today and must keep working.
func TestAdHocSubProcessWithNoConditionProceeds(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "AdHoc Open Project")

	def := adHocDefinition("claim-research-open", "")
	def.Project = &entities.Project{ID: h.projID}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "claim-research-open", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	if !h.waitingAt(ctx, t, instanceID, "decide") {
		t.Error("an ad-hoc sub-process with no completion condition did not carry on to the next step")
	}
}

// A completion condition that is already satisfied is the same story.
func TestAdHocSubProcessWithSatisfiedConditionProceeds(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "AdHoc Satisfied Project")

	def := adHocDefinition("claim-research-done", "js:reviewsDone >= 2")
	def.Project = &entities.Project{ID: h.projID}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "claim-research-done", map[string]any{"reviewsDone": 5})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	if !h.waitingAt(ctx, t, instanceID, "decide") {
		t.Error("an ad-hoc sub-process whose condition was already met did not carry on")
	}
}
