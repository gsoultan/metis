package impl

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// discountTable answers a discount computed from the price, by tier.
func discountTable(policy string, lines ...entities.DecisionRule) entities.DecisionDefinition {
	return entities.DecisionDefinition{
		Key:       "discount",
		HitPolicy: policy,
		Inputs:    []entities.DecisionInput{{ID: "in1", Expression: "tier", Type: "string"}},
		Outputs: []entities.DecisionOutput{
			{ID: "out1", Name: "discount", Type: "number", Computed: true},
			{ID: "out2", Name: "label", Type: "string"},
		},
		Rules: lines,
	}
}

func tierLine(tier string, outputs ...any) entities.DecisionRule {
	return entities.DecisionRule{Inputs: []string{tier}, Outputs: outputs}
}

func TestAComputedOutputIsEvaluatedPerHitPolicy(t *testing.T) {
	vars := map[string]any{"tier": "gold", "price": 200.0}
	tests := []struct {
		name   string
		def    entities.DecisionDefinition
		output string
		want   any
	}{
		{
			name:   "FIRST computes the line it chose",
			def:    discountTable(entities.HitPolicyFirst, tierLine(`"gold"`, "price * 0.1", "gold"), tierLine("-", "0", "none")),
			output: "discount", want: 20.0,
		},
		{
			name:   "a value beside a computed column is left as it was written",
			def:    discountTable(entities.HitPolicyFirst, tierLine(`"gold"`, "price * 0.1", "price")),
			output: "label", want: "price",
		},
		{
			name:   "a FEEL string literal is text",
			def:    discountTable(entities.HitPolicyFirst, tierLine(`"gold"`, `"ten percent"`, "x")),
			output: "discount", want: "ten percent",
		},
		{
			name:   "a number stored in a computed column is already a value",
			def:    discountTable(entities.HitPolicyFirst, tierLine(`"gold"`, 7.0, "x")),
			output: "discount", want: 7.0,
		},
		{
			name:   "a blank computed cell answers nothing",
			def:    discountTable(entities.HitPolicyFirst, tierLine(`"gold"`, " ", "x")),
			output: "discount", want: nil,
		},
		{
			name:   "UNIQUE computes its one line",
			def:    discountTable(entities.HitPolicyUnique, tierLine(`"gold"`, "price - 50", "x"), tierLine(`"silver"`, "1/0", "y")),
			output: "discount", want: 150.0,
		},
		{
			name: "COLLECT SUM adds the computed values",
			def: func() entities.DecisionDefinition {
				d := discountTable(entities.HitPolicyCollect, tierLine(`"gold"`, "price * 0.1", "a"), tierLine("-", "5", "b"))
				d.Aggregation = entities.AggregationSum
				return d
			}(),
			output: "discount", want: 25.0,
		},
		{
			name:   "RULE ORDER lists every computed value",
			def:    discountTable(entities.HitPolicyRuleOrder, tierLine(`"gold"`, "price * 0.1", "a"), tierLine("-", "price", "b")),
			output: "discount", want: []any{20.0, 200.0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluate(t, tt.def, vars).Values[tt.output]
			if !equalValues(got, tt.want) {
				t.Errorf("%s = %#v, want %#v", tt.output, got, tt.want)
			}
		})
	}
}

// TestALineNotChosenCannotFailTheDecision: under FIRST the second line is never
// read, so an expression on it that would fail is never evaluated.
func TestALineNotChosenCannotFailTheDecision(t *testing.T) {
	def := discountTable(entities.HitPolicyFirst, tierLine(`"gold"`, "price * 0.1", "a"), tierLine("-", "price +", "b"))
	if got := evaluate(t, def, map[string]any{"tier": "gold", "price": 10.0}).Values["discount"]; got != 1.0 {
		t.Errorf("discount = %v, want 1", got)
	}
}

// TestAComputedCellThatCannotBeEvaluatedIsAnError: answering null would let a
// process carry on with no discount and no word in its timeline.
func TestAComputedCellThatCannotBeEvaluatedIsAnError(t *testing.T) {
	def := discountTable(entities.HitPolicyFirst, tierLine(`"gold"`, "price +", "a"))
	_, err := NewDecisionTableEvaluator(NewFEELEvaluator()).EvaluateTable(t.Context(), def, map[string]any{"tier": "gold", "price": 1.0})
	if err == nil {
		t.Fatal("an expression that does not parse produced an answer")
	}
	if !strings.Contains(err.Error(), "line 1") || !strings.Contains(err.Error(), `"discount"`) {
		t.Errorf("error %q does not name the line and the column", err)
	}
}

// TestAComputedCellDoesNotChangeTheTable: a table version is shared by every
// evaluation of it, so the evaluated value must not be written back into it.
func TestAComputedCellDoesNotChangeTheTable(t *testing.T) {
	def := discountTable(entities.HitPolicyFirst, tierLine(`"gold"`, "price * 0.1", "a"))
	evaluate(t, def, map[string]any{"tier": "gold", "price": 10.0})
	if def.Rules[0].Outputs[0] != "price * 0.1" {
		t.Errorf("the table now holds %v", def.Rules[0].Outputs[0])
	}
}

// TestPriorityRanksAComputedOutputByItsValue: PRIORITY reads the value a line
// produces, so a computed first column is ranked by what it computed.
func TestPriorityRanksAComputedOutputByItsValue(t *testing.T) {
	def := discountTable(entities.HitPolicyPriority, tierLine("-", `"LOW"`, "a"), tierLine("-", `if price > 100 then "HIGH" else "LOW"`, "b"))
	def.Outputs[0].Values = []string{"HIGH", "LOW"}
	got := evaluate(t, def, map[string]any{"tier": "gold", "price": 200.0})
	if got.Values["discount"] != "HIGH" || got.Values["label"] != "b" {
		t.Errorf("got %v, want the HIGH line", got.Values)
	}
}

func equalValues(got, want any) bool {
	gotList, gotIsList := got.([]any)
	wantList, wantIsList := want.([]any)
	if gotIsList != wantIsList {
		return false
	}
	if !gotIsList {
		return got == want
	}
	if len(gotList) != len(wantList) {
		return false
	}
	for i := range gotList {
		if gotList[i] != wantList[i] {
			return false
		}
	}
	return true
}
