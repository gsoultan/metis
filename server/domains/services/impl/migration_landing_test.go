package impl

import (
	"slices"
	"strings"
	"testing"

	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

func namedSteps() models.ProcessDefinitionModel {
	return models.ProcessDefinitionModel{Version: 1, Nodes: []models.FlowNode{
		{ID: "opsApprove", Name: "Operations approve"},
		{ID: "gate"},
		{ID: "sub", Name: "Checks", Nodes: []models.FlowNode{{ID: "extraCheck", Name: "Extra check"}}},
	}}
}

// A passed-over instance is told which steps by the names people know them
// by, a step inside a sub-process included; one with no name has only its id.
func TestStepNamesReadAsASentenceAndNeverByIDWhereThereIsAName(t *testing.T) {
	cases := []struct {
		name string
		ids  []string
		want string
	}{
		{"one step", []string{"opsApprove"}, `"Operations approve"`},
		{"two steps", []string{"extraCheck", "opsApprove"}, `"Extra check" and "Operations approve"`},
		{"three steps", []string{"extraCheck", "gate", "opsApprove"}, `"Extra check", "gate" and "Operations approve"`},
		{"a step the version does not have", []string{"gone"}, `"gone"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stepNames(namedSteps(), c.ids); got != c.want {
				t.Errorf("stepNames(%v) = %s, want %s", c.ids, got, c.want)
			}
		})
	}
}

// Each reason names the step, the version the instance stays on, and what to
// do about it, and never an id where the step has a name.
func TestAPassedOverInstanceIsToldWhyInWords(t *testing.T) {
	source, target := namedSteps(), models.ProcessDefinitionModel{Version: 2}
	cases := []struct {
		name   string
		reason string
		want   []string
	}{
		{"nowhere to land", nowhereToLand(source, target, []string{"opsApprove"}),
			[]string{`"Operations approve"`, "version 2 has nowhere to put", "stays on version 1", "Plan the migration again"}},
		{"not decided", waitingToBeDecided(source, []string{"opsApprove"}),
			[]string{`"Operations approve"`, "no decision had settled it", "stays on version 1", "run the same migration again"}},
		{"counters would merge", countersWouldMerge(source),
			[]string{"cannot be added together", "stays on version 1", "Plan the migration again"}},
		{"not planned for", notPlannedFor(source),
			[]string{"not on version 1 when this migration was planned", "stays on version 1", "plan the migration again"}},
		{"already moved", alreadyMoved(source),
			[]string{"no longer on version 1", "had already moved it", "not moved again"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, want := range c.want {
				if !strings.Contains(c.reason, want) {
					t.Errorf("the reason does not say %q: %s", want, c.reason)
				}
			}
			if strings.Contains(c.reason, "opsApprove") {
				t.Errorf("the reason names a step by its id: %s", c.reason)
			}
		})
	}
}

// A token still on a step the migration decides is work no decision settled;
// a counter or a finished step there is not.
func TestUndecidedStepsAreTheDecidedStepsStillHoldingAToken(t *testing.T) {
	actions := map[string]servicecontracts.NodeAction{
		"opsApprove": {Kind: servicecontracts.NodeActionSkip},
		"extraCheck": {Kind: servicecontracts.NodeActionHold},
		"gate":       {Kind: servicecontracts.NodeActionCancel},
	}
	instance := models.ProcessInstanceModel{
		Tokens:         []models.Token{{NodeID: "opsApprove"}, {NodeID: "salesApprove"}, {NodeID: "extraCheck"}, {NodeID: "opsApprove"}},
		CompletedNodes: []string{"gate"},
		Joins:          map[string]int{"gate": 1},
	}
	if got := undecidedSteps(instance, actions); !slices.Equal(got, []string{"extraCheck", "opsApprove"}) {
		t.Errorf("undecidedSteps = %v, want the two decided steps that hold a token, each once, in order", got)
	}
	if got := undecidedSteps(instance, nil); len(got) != 0 {
		t.Errorf("a migration that decides nothing has no undecided step, got %v", got)
	}
	if got := nodesOf([]string{"b", "a"}, []string{"a", "c"}); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("nodesOf = %v, want each node once, in order", got)
	}
}
