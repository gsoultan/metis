package impl

import (
	"fmt"
	"maps"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// scoreTable is a score column beside a minimum column: one line, whose cells
// are the ones under test, answering "matched".
func scoreTable(scoreCell, minimumCell string) entities.DecisionDefinition {
	return entities.DecisionDefinition{
		Key:       "score",
		HitPolicy: entities.HitPolicyFirst,
		Inputs: []entities.DecisionInput{
			{ID: "in1", Label: "Score", Expression: "score", Type: "number"},
			{ID: "in2", Label: "Minimum", Expression: "minimum", Type: "number"},
		},
		Outputs: []entities.DecisionOutput{{ID: "out1", Name: "matched", Type: "boolean"}},
		Rules:   []entities.DecisionRule{{Inputs: []string{scoreCell, minimumCell}, Outputs: []any{true}}},
	}
}

func matches(t *testing.T, def entities.DecisionDefinition, vars map[string]any) bool {
	t.Helper()
	return evaluate(t, def, vars).Values["matched"] == true
}

// TestACellsOwnValueIsNotShadowedByAVariable: `_input` is the cell's own
// column, whatever the decision's variables hold. A process can carry a
// variable of any name, and one called `_input` must not turn every cell that
// uses the name into a comparison with it.
func TestACellsOwnValueIsNotShadowedByAVariable(t *testing.T) {
	vars := map[string]any{"score": 70.0, "minimum": 50.0, "_input": 1000.0}

	if !matches(t, scoreTable("= _input", "-"), vars) {
		t.Error("`= _input` did not match: _input is not the cell's own column")
	}
	if matches(t, scoreTable("< _input", "-"), vars) {
		t.Error("`< _input` matched a score of 70: _input read the variable of that name (1000), not the column")
	}
}

// TestANameIsAVariableNotAHeading: a column's heading is not a name a cell can
// use. A column is in scope under the variable it reads; the words at the top
// of the grid are for people. Here the heading "minimum" sits over a column
// reading floor, and `> minimum` means the variable minimum.
func TestANameIsAVariableNotAHeading(t *testing.T) {
	def := scoreTable("> minimum", "-")
	def.Inputs[1] = entities.DecisionInput{ID: "in2", Label: "minimum", Expression: "floor", Type: "number"}
	vars := map[string]any{"score": 70.0, "floor": 90.0, "minimum": 50.0}

	if !matches(t, def, vars) {
		t.Error("`> minimum` did not compare 70 with the variable minimum (50)")
	}
}

// TestAColumnAndAVariableOfTheSameNameAreOneValue answers "a column and a
// variable share a name: which does a cell read?". They are one value. A
// column's expression is the name of the variable it reads, so the minimum
// column and the variable minimum are the same entry in the variables the
// decision was evaluated with — nothing converts or copies it on the way, so a
// cell naming it and the column's own cell see the same thing.
func TestAColumnAndAVariableOfTheSameNameAreOneValue(t *testing.T) {
	vars := map[string]any{"score": 70.0, "minimum": 50.0}

	if !matches(t, scoreTable("> minimum", "50"), vars) {
		t.Error("the score cell and the minimum column's own cell saw different minimums")
	}
	vars["minimum"] = 80.0
	if matches(t, scoreTable("> minimum", "-"), vars) {
		t.Error("`> minimum` matched 70 against a minimum of 80")
	}
}

// BenchmarkATableBesideALargeOrder is a banded table of a hundred lines,
// evaluated with a process's variables that include an order of five thousand
// lines no cell reads — what a business rule task with no input mapping hands
// it. Every cell now sees those variables; this is what that costs.
func BenchmarkATableBesideALargeOrder(b *testing.B) {
	rules := make([]entities.DecisionRule, 100)
	for i := range rules {
		rules[i] = entities.DecisionRule{Inputs: []string{fmt.Sprintf("> %d", 100-i), "> minimum"}, Outputs: []any{true}}
	}
	def := scoreTable("-", "-")
	def.HitPolicy = entities.HitPolicyCollect
	def.Aggregation = entities.AggregationCount
	def.Rules = rules

	lines := make([]any, 5000)
	for i := range lines {
		lines[i] = map[string]any{"sku": fmt.Sprintf("SKU-%d", i), "quantity": float64(i)}
	}
	vars := map[string]any{"score": 70.0, "minimum": 50.0, "order": map[string]any{"lines": lines}}

	evaluator := NewDecisionTableEvaluator(NewFEELEvaluator())
	for b.Loop() {
		if _, err := evaluator.EvaluateTable(b.Context(), def, vars); err != nil {
			b.Fatal(err)
		}
	}
}

// TestEvaluatingATableLeavesItsVariablesAlone: a cell reads the decision's
// variables and writes nothing into them. The map belongs to the caller — a
// process's variables, a saved example — and binding `_input` into it would
// leak into whatever the caller does next.
func TestEvaluatingATableLeavesItsVariablesAlone(t *testing.T) {
	vars := map[string]any{"score": 70.0, "minimum": 50.0}
	before := maps.Clone(vars)

	evaluate(t, scoreTable("> minimum", "-"), vars)

	if !maps.Equal(vars, before) {
		t.Errorf("variables after evaluation = %v, want them unchanged: %v", vars, before)
	}
}
