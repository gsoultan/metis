package impl

import (
	"strings"
	"testing"

	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// everyKindOfNode is one node of every type the engine has a handler for, and
// the two that are not steps at all, with where its handler leaves the token:
// on the node (an instance can wait there, and a decision can be taken) or
// not (the decision would be made about nobody). The handlers are in
// server/domains/handlers/impl; the factory lists the types.
var everyKindOfNode = []struct {
	node  models.FlowNode
	waits bool
	// says is what the refusal must say of a node nobody waits at.
	says string
}{
	// Waits: the handler returns with the token still on the node.
	{models.FlowNode{ID: "approve", Name: "Approve", Type: models.UserTask}, true, ""},
	{models.FlowNode{ID: "pack", Name: "Pack", Type: models.ManualTask}, true, ""},
	{models.FlowNode{ID: "charge", Name: "Charge", Type: models.ServiceTask}, true, ""},
	{models.FlowNode{ID: "wait", Name: "Wait for the reply", Type: models.IntermediateCatchEvent}, true, ""},
	{models.FlowNode{ID: "pause", Name: "Pause", Type: models.TimerEvent}, true, ""},
	{models.FlowNode{ID: "call", Name: "Call the other process", Type: models.CallActivity}, true, ""},
	{models.FlowNode{ID: "research", Name: "Research", Type: models.SubProcess, IsAdHoc: true}, true, ""},
	{models.FlowNode{ID: "join", Name: "Join", Type: models.ParallelGateway}, true, ""},
	{models.FlowNode{ID: "merge", Name: "Merge", Type: models.InclusiveGateway}, true, ""},
	// Not shown never to wait: its handler does not advance it itself.
	{models.FlowNode{ID: "escalate", Name: "Escalate", Type: models.EscalationThrowEvent}, true, ""},

	// Does not wait: the handler takes the token off, or advances at once.
	{models.FlowNode{ID: "begin", Name: "Request received", Type: models.StartEvent}, false, "start event"},
	{models.FlowNode{ID: "done", Name: "Done", Type: models.EndEvent}, false, "end event"},
	{models.FlowNode{ID: "stop", Name: "Stop", Type: models.TerminateEndEvent}, false, "end event"},
	{models.FlowNode{ID: "failed", Name: "Failed", Type: models.ErrorEndEvent}, false, "end event"},
	{models.FlowNode{ID: "route", Name: "Route", Type: models.ExclusiveGateway}, false, "gateway"},
	{models.FlowNode{ID: "race", Name: "Race", Type: models.EventBasedGateway}, false, "gateway"},
	{models.FlowNode{ID: "split", Name: "Split", Type: models.ParallelGateway}, false, "gateway"},
	{models.FlowNode{ID: "fan", Name: "Fan out", Type: models.InclusiveGateway}, false, "gateway"},
	{models.FlowNode{ID: "compute", Name: "Compute", Type: models.ScriptTask}, false, "script task"},
	{models.FlowNode{ID: "decide", Name: "Decide", Type: models.BusinessRuleTask}, false, "business rule task"},
	{models.FlowNode{ID: "notify", Name: "Notify", Type: models.IntermediateThrowEvent}, false, "throws"},
	{models.FlowNode{ID: "signal", Name: "Signal", Type: models.SignalEvent}, false, "throws"},
	{models.FlowNode{ID: "send", Name: "Send", Type: models.MessageEvent}, false, "throws"},
	{models.FlowNode{ID: "undo", Name: "Undo", Type: models.CompensationThrowEvent}, false, "throws"},
	{models.FlowNode{ID: "withdrew", Name: "The customer withdrew", Type: models.BoundaryEvent, AttachedToRef: "approve"}, false, "boundary event"},
	{models.FlowNode{ID: "orphan", Type: models.BoundaryEvent, AttachedToRef: "gone"}, false, "boundary event"},
	{
		models.FlowNode{ID: "checks", Name: "Checks", Type: models.SubProcess, Nodes: []models.FlowNode{
			{ID: "checksStart", Type: models.StartEvent, ParentID: "checks"},
			{ID: "check", Name: "Check the request", Type: models.UserTask, ParentID: "checks"},
			{ID: "deeper", Name: "Deeper", Type: models.SubProcess, ParentID: "checks", Nodes: []models.FlowNode{
				{ID: "verify", Name: "Verify", Type: models.UserTask, ParentID: "deeper"},
			}},
		}},
		false, `decide the steps inside it instead: "Check the request", "Verify"`,
	},
	{models.FlowNode{ID: "onRecall", Name: "On recall", Type: models.SubProcess, IsEventSubProcess: true}, false, "sub-process"},
	{models.FlowNode{ID: "sales", Name: "Sales", Type: models.Lane}, false, "lane"},
	{models.FlowNode{ID: "company", Name: "Company", Type: models.Pool}, false, "pool"},
}

// incomingOfEveryKind is how many flows arrive at each node: the two gateways
// that join have two, and the two that only split have one.
var incomingOfEveryKind = map[string]int{"join": 2, "merge": 2, "split": 1, "fan": 1}

// A decision is refused on every kind of node the engine never leaves a token
// on, whichever decision it is, and accepted on every kind it does. The
// refusal names the step by its name and says what kind of node it is.
func TestADecisionIsRefusedWhereNoInstanceEverWaits(t *testing.T) {
	t.Parallel()
	source := map[string]models.FlowNode{}
	for _, kind := range everyKindOfNode {
		source[kind.node.ID] = kind.node
	}
	for _, kind := range everyKindOfNode {
		for _, action := range []servicecontracts.NodeActionKind{servicecontracts.NodeActionCancel, servicecontracts.NodeActionHold} {
			t.Run(string(kind.node.Type)+"/"+kind.node.ID+"/"+string(action), func(t *testing.T) {
				actions := map[string]servicecontracts.NodeAction{kind.node.ID: {Kind: action, Reason: "the step was dropped"}}
				refusals := (&migrationService{}).actionRefusals(source, nil, nil, actions, incomingOfEveryKind)
				if kind.waits {
					if len(refusals) != 0 {
						t.Fatalf("a %s at a node an instance can wait at was refused: %v", action, refusals)
					}
					return
				}
				if len(refusals) != 1 {
					t.Fatalf("want the one refusal, got %v", refusals)
				}
				name := kind.node.Name
				if name == "" {
					name = kind.node.ID
				}
				for _, want := range []string{string(action) + ` of "` + name + `" cannot be taken`, kind.says, "about nobody"} {
					if !strings.Contains(refusals[0], want) {
						t.Errorf("the refusal does not say %q: %s", want, refusals[0])
					}
				}
			})
		}
	}
}

// Every type the stored model declares is in the table above, so a type added
// to the engine is decided here on purpose and not by falling through.
func TestEveryNodeTypeIsDecidedOnPurpose(t *testing.T) {
	t.Parallel()
	covered := map[models.NodeType]bool{}
	for _, kind := range everyKindOfNode {
		covered[kind.node.Type] = true
	}
	for _, declared := range []models.NodeType{
		models.StartEvent, models.EndEvent, models.UserTask, models.ServiceTask, models.ExclusiveGateway,
		models.ParallelGateway, models.InclusiveGateway, models.ScriptTask, models.IntermediateCatchEvent,
		models.IntermediateThrowEvent, models.CallActivity, models.ManualTask, models.BusinessRuleTask,
		models.SubProcess, models.BoundaryEvent, models.EventBasedGateway, models.MessageEvent, models.SignalEvent,
		models.TimerEvent, models.ErrorEndEvent, models.TerminateEndEvent, models.EscalationThrowEvent,
		models.CompensationThrowEvent, models.Pool, models.Lane,
	} {
		if !covered[declared] {
			t.Errorf("node type %q is not in the table of where an instance waits", declared)
		}
	}
}

// The boundary event's refusal says what works: name it with the step it is
// attached to.
func TestABoundaryEventsRefusalSaysToNameItWithItsStep(t *testing.T) {
	t.Parallel()
	source := map[string]models.FlowNode{
		"approve":  {ID: "approve", Name: "Approve the request", Type: models.UserTask},
		"withdrew": {ID: "withdrew", Name: "The customer withdrew", Type: models.BoundaryEvent, AttachedToRef: "approve"},
		"orphan":   {ID: "orphan", Type: models.BoundaryEvent, AttachedToRef: "gone"},
	}
	hold := servicecontracts.NodeAction{Kind: servicecontracts.NodeActionHold, Reason: "the step was dropped"}
	for node, want := range map[string]string{
		"withdrew": `name the event together with "Approve the request"`,
		"orphan":   "name the event together with the step it is attached to",
	} {
		refusals := (&migrationService{}).actionRefusals(source, nil, nil, map[string]servicecontracts.NodeAction{node: hold}, nil)
		if len(refusals) != 1 || !strings.Contains(refusals[0], want) {
			t.Errorf("the refusal for %s should say %q: %v", node, want, refusals)
		}
	}
}

// A boundary event named together with the step it is attached to is left as
// it was: what waits on the event ends with its step, and naming it is how the
// plan is told so. Named with any other step, it is refused.
func TestABoundaryEventMayBeNamedWithTheStepItIsAttachedTo(t *testing.T) {
	t.Parallel()
	source := map[string]models.FlowNode{
		"approve":  {ID: "approve", Name: "Approve the request", Type: models.UserTask, Outgoing: []string{"f1"}},
		"sign":     {ID: "sign", Name: "Sign", Type: models.UserTask, Outgoing: []string{"f2"}},
		"deadline": {ID: "deadline", Name: "Three days passed", Type: models.BoundaryEvent, AttachedToRef: "approve", Outgoing: []string{"f3"}},
	}
	hold := servicecontracts.NodeAction{Kind: servicecontracts.NodeActionHold, Reason: "the approval was dropped"}

	withItsStep := map[string]servicecontracts.NodeAction{"approve": hold, "deadline": hold}
	if refusals := (&migrationService{}).actionRefusals(source, nil, nil, withItsStep, nil); len(refusals) != 0 {
		t.Errorf("a deadline named with the approval it is on was refused: %v", refusals)
	}
	withAnother := map[string]servicecontracts.NodeAction{"sign": hold, "deadline": hold}
	refusals := (&migrationService{}).actionRefusals(source, nil, nil, withAnother, nil)
	if len(refusals) != 1 || !strings.Contains(refusals[0], `"Three days passed" cannot be taken`) {
		t.Errorf("a deadline named with a step it is not on should be refused, once: %v", refusals)
	}
}
