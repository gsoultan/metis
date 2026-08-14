package bpmn_test

import (
	"strings"
	"testing"

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

// An ad-hoc sub-process that cannot be completed must report that, not hang.
func TestAdHocSubProcessReportsThatItCannotBeAdvanced(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "AdHoc Blocked Project")

	def := adHocDefinition("claim-research", "js:reviewsDone >= 2")
	def.Project = &entities.Project{ID: h.projID}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	_, err := h.svc.StartProcess(ctx, h.projID, "claim-research", map[string]any{"reviewsDone": 0})

	if err == nil {
		t.Fatal("an ad-hoc sub-process with an unmet completion condition was accepted; the instance is parked with no way to advance and no incident raised")
	}
	if !strings.Contains(err.Error(), "research") {
		t.Errorf("the error does not name the ad-hoc sub-process that is stuck: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "activat") {
		t.Errorf("the error does not explain that no activation path exists: %v", err)
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
