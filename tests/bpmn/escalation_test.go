package bpmn_test

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Escalation is how a sub-process tells its parent "this needs attention" —
// a discount over the approval limit, a delivery running late. The parent
// catches it on a boundary event carrying the matching escalation code.
//
// The code is the whole point: it says which situation is being reported. A
// handler that catches an escalation it was not written for is worse than one
// that catches nothing, because the process carries on down a path chosen for a
// different problem.

// escalationDefinition builds a sub-process that raises escalationCode, with
// whatever boundary events the test attaches to it.
func escalationDefinition(projID interface{ String() string }, key, escalationCode string, boundaries []*entities.Node, extraFlows []*entities.SequenceFlow) entities.ProcessDefinition {
	nodes := []*entities.Node{
		{ID: "start", Type: entities.StartEvent},
		{ID: "sub", Type: entities.SubProcess, Nodes: []*entities.Node{
			{ID: "sub-start", Type: entities.StartEvent, ParentID: "sub"},
			{ID: "esc-throw", Type: entities.EscalationThrowEvent, ParentID: "sub", Properties: map[string]any{
				"escalation_code": escalationCode,
			}},
			{ID: "sub-end", Type: entities.EndEvent, ParentID: "sub"},
		}, Flows: []*entities.SequenceFlow{
			{ID: "sf1", SourceRef: "sub-start", TargetRef: "esc-throw"},
			{ID: "sf2", SourceRef: "esc-throw", TargetRef: "sub-end"},
		}},
		{ID: "end", Type: entities.EndEvent},
	}
	nodes = append(nodes, boundaries...)

	flows := []*entities.SequenceFlow{
		{ID: "f1", SourceRef: "start", TargetRef: "sub"},
		{ID: "f2", SourceRef: "sub", TargetRef: "end"},
	}
	flows = append(flows, extraFlows...)

	return entities.ProcessDefinition{Key: key, Nodes: nodes, Flows: flows}
}

// An error boundary event carries no escalation code. It must not catch an
// escalation — it was written for a failure, not for a business exception.
//
// This is the case the "empty code matches anything" rule gets wrong:
// GetBoundaryEvents returns boundary events of every kind, and error, timer,
// message and compensation events all report an empty escalation_code.
func TestEscalationIsNotCaughtByAnErrorBoundaryEvent(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Escalation Wrong Handler Project")

	def := escalationDefinition(h.projID, "escalation-vs-error", "BUDGET_EXCEEDED",
		[]*entities.Node{
			{ID: "err-catch", Type: entities.BoundaryEvent, AttachedToRef: "sub", Properties: map[string]any{
				"error_code": "PAYMENT_FAILED",
			}},
			{ID: "handle-payment-failure", Type: entities.UserTask, Name: "Handle the payment failure"},
		},
		[]*entities.SequenceFlow{
			{ID: "f3", SourceRef: "err-catch", TargetRef: "handle-payment-failure"},
		})
	def.Project = &entities.Project{ID: h.projID}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "escalation-vs-error", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	if h.waitingAt(ctx, t, instanceID, "handle-payment-failure") {
		t.Error("a BUDGET_EXCEEDED escalation was routed into the PAYMENT_FAILED error handler")
	}
}

// The handler whose code matches must still catch it.
func TestEscalationIsCaughtByTheMatchingHandler(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Escalation Match Project")

	def := escalationDefinition(h.projID, "escalation-match", "BUDGET_EXCEEDED",
		[]*entities.Node{
			{ID: "esc-catch", Type: entities.BoundaryEvent, AttachedToRef: "sub", Properties: map[string]any{
				"escalation_code": "BUDGET_EXCEEDED",
			}},
			{ID: "approve-budget", Type: entities.UserTask, Name: "Approve the extra budget"},
		},
		[]*entities.SequenceFlow{
			{ID: "f3", SourceRef: "esc-catch", TargetRef: "approve-budget"},
		})
	def.Project = &entities.Project{ID: h.projID}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "escalation-match", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	if !h.waitingAt(ctx, t, instanceID, "approve-budget") {
		t.Error("the escalation did not reach the handler carrying its own code")
	}
}

// BPMN 2.0 says an escalation boundary event with no escalation code catches any
// escalation. That rule is kept — it is only scoped to escalation events now,
// rather than to every boundary event on the activity.
func TestEscalationBoundaryWithNoCodeCatchesAnything(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Escalation Catch All Project")

	def := escalationDefinition(h.projID, "escalation-catch-all", "BUDGET_EXCEEDED",
		[]*entities.Node{
			{ID: "esc-catch-any", Type: entities.BoundaryEvent, AttachedToRef: "sub", Properties: map[string]any{
				"event_type": "escalation",
			}},
			{ID: "review-anything", Type: entities.UserTask, Name: "Review whatever came up"},
		},
		[]*entities.SequenceFlow{
			{ID: "f3", SourceRef: "esc-catch-any", TargetRef: "review-anything"},
		})
	def.Project = &entities.Project{ID: h.projID}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "escalation-catch-all", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	if !h.waitingAt(ctx, t, instanceID, "review-anything") {
		t.Error("an escalation boundary event with no code did not catch the escalation")
	}
}

// A handler for a different escalation must not catch this one.
func TestEscalationIsNotCaughtByADifferentCode(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Escalation Mismatch Project")

	def := escalationDefinition(h.projID, "escalation-mismatch", "BUDGET_EXCEEDED",
		[]*entities.Node{
			{ID: "esc-catch-delay", Type: entities.BoundaryEvent, AttachedToRef: "sub", Properties: map[string]any{
				"escalation_code": "DELIVERY_DELAYED",
			}},
			{ID: "chase-courier", Type: entities.UserTask, Name: "Chase the courier"},
		},
		[]*entities.SequenceFlow{
			{ID: "f3", SourceRef: "esc-catch-delay", TargetRef: "chase-courier"},
		})
	def.Project = &entities.Project{ID: h.projID}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "escalation-mismatch", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	if h.waitingAt(ctx, t, instanceID, "chase-courier") {
		t.Error("a BUDGET_EXCEEDED escalation was caught by the DELIVERY_DELAYED handler")
	}
}
