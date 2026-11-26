package logic

import (
	"reflect"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A computed output cell is an expression evaluated with the decision's
// variables, so what it names is read. A value in a column that is not
// computed is text, and reads nothing.
func TestDecisionTableReadsComputedOutputs(t *testing.T) {
	t.Parallel()
	outputs := []entities.DecisionOutput{{Name: "discount", Computed: true}, {Name: "label"}}
	cases := []struct {
		name       string
		outputs    []any
		names      []string
		analysable bool
	}{
		{name: "an expression is read", outputs: []any{"price * rate", "plain"}, names: []string{"price", "rate", "tier"}, analysable: true},
		{name: "a value is not", outputs: []any{12.0, "price"}, names: []string{"tier"}, analysable: true},
		{name: "a blank cell reads nothing", outputs: []any{"  ", "x"}, names: []string{"tier"}, analysable: true},
		{name: "one that does not parse is not analysable", outputs: []any{"price +", "x"}, names: []string{"tier"}, analysable: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			table := entities.DecisionDefinition{
				Inputs:  []entities.DecisionInput{column("tier")},
				Outputs: outputs,
				Rules:   []entities.DecisionRule{{Inputs: []string{"-"}, Outputs: tc.outputs}},
			}
			names, _, analysable := DecisionTableReads(table)
			if !reflect.DeepEqual(names, tc.names) || analysable != tc.analysable {
				t.Errorf("got %v analysable=%v, want %v analysable=%v", names, analysable, tc.names, tc.analysable)
			}
		})
	}
}
