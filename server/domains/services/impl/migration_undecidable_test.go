package impl

import (
	"strings"
	"testing"

	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// A decision is refused on the two kinds of node that keep work without ever
// keeping a token, whichever decision it is, and on nothing else. The refusal
// names the step by its name, and for a boundary event the step to decide
// instead.
func TestADecisionIsRefusedWhereNoInstanceEverWaits(t *testing.T) {
	t.Parallel()
	source := map[string]models.FlowNode{
		"begin":    {ID: "begin", Name: "Request received", Type: models.StartEvent, Outgoing: []string{"f1"}},
		"approve":  {ID: "approve", Name: "Approve the request", Type: models.UserTask, Outgoing: []string{"f2"}},
		"withdrew": {ID: "withdrew", Name: "The customer withdrew", Type: models.BoundaryEvent, AttachedToRef: "approve", Outgoing: []string{"f3"}},
		"orphan":   {ID: "orphan", Type: models.BoundaryEvent, AttachedToRef: "gone", Outgoing: []string{"f4"}},
		"wait":     {ID: "wait", Name: "Wait for the reply", Type: models.IntermediateCatchEvent, Outgoing: []string{"f5"}},
	}
	cases := []struct {
		node string
		want []string
	}{
		{"withdrew", []string{`"The customer withdrew" cannot be taken`, "boundary event", `decide "Approve the request", and`}},
		{"orphan", []string{`"orphan" cannot be taken`, "boundary event", "decide the step it is attached to, and"}},
		{"begin", []string{`"Request received" cannot be taken`, "start event"}},
		{"approve", nil},
		{"wait", nil},
	}
	kinds := []servicecontracts.NodeActionKind{servicecontracts.NodeActionCancel, servicecontracts.NodeActionHold}
	for _, c := range cases {
		for _, kind := range kinds {
			t.Run(c.node+"/"+string(kind), func(t *testing.T) {
				actions := map[string]servicecontracts.NodeAction{c.node: {Kind: kind, Reason: "the step was dropped"}}
				refusals := (&migrationService{}).actionRefusals(source, nil, nil, actions)
				if len(c.want) == 0 {
					if len(refusals) != 0 {
						t.Fatalf("a %s at a step an instance can wait at was refused: %v", kind, refusals)
					}
					return
				}
				if len(refusals) != 1 {
					t.Fatalf("want the one refusal, got %v", refusals)
				}
				for _, want := range append(c.want, string(kind)+" of ") {
					if !strings.Contains(refusals[0], want) {
						t.Errorf("the refusal does not say %q: %s", want, refusals[0])
					}
				}
			})
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
	if refusals := (&migrationService{}).actionRefusals(source, nil, nil, withItsStep); len(refusals) != 0 {
		t.Errorf("a deadline named with the approval it is on was refused: %v", refusals)
	}
	withAnother := map[string]servicecontracts.NodeAction{"sign": hold, "deadline": hold}
	refusals := (&migrationService{}).actionRefusals(source, nil, nil, withAnother)
	if len(refusals) != 1 || !strings.Contains(refusals[0], `"Three days passed" cannot be taken`) {
		t.Errorf("a deadline named with a step it is not on should be refused, once: %v", refusals)
	}
}
