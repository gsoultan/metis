package decision_test

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// An evaluation should say which line of the table produced it.
//
// "Why did it decide that?" is the question a decision table exists to answer,
// and the result carried only the values, so the answer was never available.
// The decision editor already asks for it — it highlights the matching row from
// a matchedRuleIndexes field — but nothing ever sent one, so the highlight
// never appeared and the tester showed an answer with no reasoning.
func TestEvaluateTable_ReportsWhichRuleMatched(t *testing.T) {
	evaluator := serviceimpl.NewDecisionTableEvaluator(serviceimpl.NewFEELEvaluator())
	def := expenseTable()

	cases := []struct {
		amount    float64
		wantLevel string
		wantRule  int
	}{
		{40, "auto", 0},
		{500, "manager", 1},
		{2400, "director", 2},
	}

	for _, tc := range cases {
		got, err := evaluator.EvaluateTable(t.Context(), def, map[string]any{"amount": tc.amount})
		if err != nil {
			t.Fatalf("amount %v: %v", tc.amount, err)
		}
		if got.Values["approvalLevel"] != tc.wantLevel {
			t.Errorf("amount %v gave %v, want %q", tc.amount, got.Values["approvalLevel"], tc.wantLevel)
		}
		if len(got.MatchedRules) != 1 || got.MatchedRules[0] != tc.wantRule {
			t.Errorf("amount %v matched rules %v, want [%d] — the editor cannot show which line applied",
				tc.amount, got.MatchedRules, tc.wantRule)
		}
	}
}

// COLLECT applies every matching line, so it should report every one.
func TestEvaluateTable_ReportsEveryRuleUnderCollect(t *testing.T) {
	evaluator := serviceimpl.NewDecisionTableEvaluator(serviceimpl.NewFEELEvaluator())
	def := expenseTable()
	def.HitPolicy = entities.HitPolicyCollect

	got, err := evaluator.EvaluateTable(t.Context(), def, map[string]any{"amount": float64(40)})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	// 40 is under 100 and under 1000, and the last line matches anything.
	if len(got.MatchedRules) != 3 {
		t.Fatalf("matched rules %v, want all three lines", got.MatchedRules)
	}
}

// Nothing matching is a real answer, and an empty list is how it is said.
func TestEvaluateTable_ReportsNoRulesWhenNoneMatch(t *testing.T) {
	evaluator := serviceimpl.NewDecisionTableEvaluator(serviceimpl.NewFEELEvaluator())
	def := entities.DecisionDefinition{
		Key:       "narrow",
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []entities.DecisionInput{{ID: "in", Expression: "amount", Type: "number"}},
		Outputs:   []entities.DecisionOutput{{ID: "out", Name: "band", Type: "string"}},
		Rules:     []entities.DecisionRule{{ID: "r1", Inputs: []string{"< 10"}, Outputs: []any{"tiny"}}},
	}

	got, err := evaluator.EvaluateTable(t.Context(), def, map[string]any{"amount": float64(99)})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(got.MatchedRules) != 0 {
		t.Errorf("matched rules %v, want none", got.MatchedRules)
	}
}

// The table from docs/data-flow.md.
func expenseTable() entities.DecisionDefinition {
	return entities.DecisionDefinition{
		Key:       "expense-approval-level",
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []entities.DecisionInput{{ID: "in_amount", Label: "Amount", Expression: "amount", Type: "number"}},
		Outputs: []entities.DecisionOutput{
			{ID: "out_level", Name: "approvalLevel", Type: "string"},
			{ID: "out_approver", Name: "approver", Type: "string"},
		},
		Rules: []entities.DecisionRule{
			{ID: "r1", Inputs: []string{"< 100"}, Outputs: []any{"auto", "system"}},
			{ID: "r2", Inputs: []string{"< 1000"}, Outputs: []any{"manager", "line-manager"}},
			{ID: "r3", Inputs: []string{"-"}, Outputs: []any{"director", "finance-director"}},
		},
	}
}
