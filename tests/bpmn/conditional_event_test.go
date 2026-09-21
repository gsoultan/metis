package bpmn_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// parkedOn reports whether the instance holds a token on nodeID.
//
// waitingAt is not the right question here: it looks for an open *task*, and a
// conditional event creates none — it is the token alone that records the wait.
func parkedOn(ctx context.Context, t *testing.T, h engineHarness, instanceID uuid.UUID, nodeID string) bool {
	t.Helper()

	instance, err := h.svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	for i := range instance.Tokens {
		if instance.Tokens[i].Node != nil && instance.Tokens[i].Node.ID == nodeID {
			return true
		}
	}
	return false
}

// A conditional event waits until something about the process becomes true.
//
// It is the BPMN element for "carry on once the total is high enough" or "once
// the paperwork is complete" — a wait with no clock and no inbound message.
// Nothing outside the process will ever wake it: the only thing that can make
// its condition true is another part of the same instance changing a variable.
//
// Before this the engine had no conditional event at all. A catch event
// carrying a condition was handed to the *timer* service as though the
// condition were a duration, and a catch event with nothing on it returned
// success while leaving its token in place — an instance that stopped for ever
// with no incident, no log line and nothing to investigate.

// conditionalDefinition parks on a conditional event while a parallel branch
// does the work that satisfies it.
//
// The two branches matter. A conditional event on the only branch could never
// be satisfied by anything, so a test with one branch would pass against an
// implementation that simply advanced regardless.
func conditionalDefinition(key, condition string) entities.ProcessDefinition {
	return entities.ProcessDefinition{
		Key: key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "split", Type: entities.ParallelGateway},

			// Branch one waits for the condition.
			{
				ID: "wait", Type: entities.IntermediateCatchEvent,
				Name:       "Wait until the claim is funded",
				Properties: map[string]any{"condition_expression": condition, "event_type": "conditional"},
			},
			{ID: "settle", Type: entities.UserTask, Name: "Settle the claim"},

			// Branch two does the work that satisfies it, in two steps so that
			// the condition can be false after the first and true after the
			// second. One step would not distinguish "waits correctly" from
			// "advances on any change at all".
			{ID: "fund", Type: entities.UserTask, Name: "Approve the funds"},
			{ID: "topup", Type: entities.UserTask, Name: "Approve the rest"},

			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "split"},
			{ID: "f2", SourceRef: "split", TargetRef: "wait"},
			{ID: "f3", SourceRef: "wait", TargetRef: "settle"},
			{ID: "f4", SourceRef: "split", TargetRef: "fund"},
			{ID: "f5", SourceRef: "fund", TargetRef: "topup"},
			{ID: "f6", SourceRef: "settle", TargetRef: "end"},
		},
	}
}

// TestConditionalEventWaitsAndThenProceeds is the whole behaviour: it holds
// while the condition is false, and moves on when another part of the process
// makes it true.
func TestConditionalEventWaitsAndThenProceeds(t *testing.T) {
	h := newEngineHarness(t, "Conditional Event Project")
	ctx := h.Ctx()

	def := conditionalDefinition("claim-funding", "funded >= 1000")
	def.Project = &entities.Project{ID: h.projID}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "claim-funding", map[string]any{"funded": 0})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	if !parkedOn(ctx, t, h, instanceID, "wait") {
		t.Fatal("the process is not parked on the conditional event")
	}
	if h.waitingAt(ctx, t, instanceID, "settle") {
		t.Fatal("the process ran past a condition that is false")
	}

	// Not enough: the condition is still false, so the wait holds.
	completeTaskAt(ctx, t, h, instanceID, "fund", map[string]any{"funded": 500})
	if h.waitingAt(ctx, t, instanceID, "settle") {
		t.Fatal("the process carried on while the condition was still false")
	}
	if !parkedOn(ctx, t, h, instanceID, "wait") {
		t.Fatal("the conditional event lost its token without its condition being met")
	}

	// Enough. Nothing addresses the waiting branch directly — no signal, no
	// message, no timer — so the only thing that can move it is the engine
	// re-reading the condition after this advance changed a variable.
	completeTaskAt(ctx, t, h, instanceID, "topup", map[string]any{"funded": 1500})

	if parkedOn(ctx, t, h, instanceID, "wait") {
		t.Error("the condition became true and the conditional event still holds its token")
	}
	if !h.waitingAt(ctx, t, instanceID, "settle") {
		t.Error("the condition became true but the process did not carry on past the wait")
	}
}

// TestConditionalEventAlreadyTrueDoesNotWait covers the arrival case. There is
// nothing to wait for if the condition already holds, and parking anyway would
// hang the instance for ever — nothing else is going to change a variable that
// is already right.
func TestConditionalEventAlreadyTrueDoesNotWait(t *testing.T) {
	h := newEngineHarness(t, "Conditional Event Immediate Project")
	ctx := h.Ctx()

	def := conditionalDefinition("claim-already-funded", "funded >= 1000")
	def.Project = &entities.Project{ID: h.projID}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "claim-already-funded", map[string]any{"funded": 5000})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	if !h.waitingAt(ctx, t, instanceID, "settle") {
		t.Error("the condition was already true on arrival but the process stopped at the event")
	}
}

// TestCatchEventWithNothingToWaitForIsRefused is the silent hang made loud.
//
// A catch event exists to wait for something. One that names nothing to wait
// for used to park its token and report success, so the instance stopped
// permanently with no incident attached — the one outcome an orchestrator
// running other people's obligations cannot afford, because there is nothing
// to investigate and no sign anything is wrong.
func TestCatchEventWithNothingToWaitForIsRefused(t *testing.T) {
	h := newEngineHarness(t, "Empty Catch Event Project")
	ctx := h.Ctx()

	def := entities.ProcessDefinition{
		Key:     "empty-catch",
		Project: &entities.Project{ID: h.projID},
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "wait", Type: entities.IntermediateCatchEvent, Name: "Wait"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "wait"},
			{ID: "f2", SourceRef: "wait", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	_, err := h.svc.StartProcess(ctx, h.projID, "empty-catch", nil)
	if err == nil {
		t.Fatal("a catch event with nothing to wait for was accepted; the instance would hang for ever")
	}
	if !strings.Contains(err.Error(), "wait") {
		t.Errorf("the refusal does not name the node that cannot be waited on: %v", err)
	}
}
