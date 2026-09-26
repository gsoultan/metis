package decision_test

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A decision cell reads the rest of the case, end to end: a process's
// variables go through a business rule task into a table whose cells compare
// one of them with another. The conformance corpus proves the evaluator; this
// proves the variables reach it on the path a running process takes.

// passMark passes a score above the minimum beside it and fails the rest.
func passMark(key string) entities.DecisionDefinition {
	return entities.DecisionDefinition{
		Key:       key,
		Name:      "Pass mark",
		HitPolicy: entities.HitPolicyFirst,
		Inputs: []entities.DecisionInput{
			{ID: "i1", Label: "Score", Expression: "score", Type: "number"},
			{ID: "i2", Label: "Minimum", Expression: "minimum", Type: "number"},
		},
		Outputs: []entities.DecisionOutput{{ID: "o1", Name: "verdict", Type: "string"}},
		Rules: []entities.DecisionRule{
			{ID: "pass", Inputs: []string{"> minimum", "-"}, Outputs: []any{"PASS"}},
			{ID: "fail", Inputs: []string{"-", "-"}, Outputs: []any{"FAIL"}},
		},
	}
}

func TestAProcessDecisionComparesTwoOfItsVariables(t *testing.T) {
	h := newBusinessRuleHarness(t)

	tests := []struct {
		key   string
		score float64
		want  string
	}{
		{key: "pass-mark-above", score: 70, want: "PASS"},
		{key: "pass-mark-below", score: 40, want: "FAIL"},
	}
	for _, tc := range tests {
		instance := h.runDecision(t, passMark(tc.key), map[string]any{"score": tc.score, "minimum": 50.0})
		if got := instance.Variables["verdict"]; got != tc.want {
			t.Errorf("a score of %v against a minimum of 50 decided %v, want %s", tc.score, got, tc.want)
		}
	}
}

// A process variable the table has no column for is in scope too: a limit the
// process looked up earlier, compared with the amount the table does test.
func TestAProcessDecisionComparesWithAVariableItHasNoColumnFor(t *testing.T) {
	h := newBusinessRuleHarness(t)
	table := entities.DecisionDefinition{
		Key:       "within-limit",
		Name:      "Within the credit limit",
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []entities.DecisionInput{{ID: "i1", Label: "Amount", Expression: "amount", Type: "number"}},
		Outputs:   []entities.DecisionOutput{{ID: "o1", Name: "route", Type: "string"}},
		Rules: []entities.DecisionRule{
			{ID: "over", Inputs: []string{"> credit_limit"}, Outputs: []any{"REVIEW"}},
			{ID: "within", Inputs: []string{"-"}, Outputs: []any{"AUTO"}},
		},
	}

	instance := h.runDecision(t, table, map[string]any{"amount": 1200.0, "credit_limit": 1000.0})
	if got := instance.Variables["route"]; got != "REVIEW" {
		t.Errorf("an amount of 1200 over a credit limit of 1000 was routed %v, want REVIEW", got)
	}
}
