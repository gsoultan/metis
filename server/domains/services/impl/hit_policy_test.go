package impl

import (
	"strings"
	"testing"

	"github.com/gsoultan/gobpm/server/domains/entities"
)

// severityTable is one table reused across policies: three lines, two of which
// match an input of 50. Only the hit policy changes, which is exactly the
// property being tested — the same matches, ranked differently.
func severityTable(policy, aggregation string) entities.DecisionDefinition {
	return entities.DecisionDefinition{
		Key:         "severity",
		HitPolicy:   policy,
		Aggregation: aggregation,
		Inputs:      []entities.DecisionInput{{ID: "in1", Expression: "amount", Type: "number"}},
		Outputs: []entities.DecisionOutput{{
			ID:   "out1",
			Name: "severity",
			Type: "string",
			// Most important first: this list is what PRIORITY and OUTPUT ORDER
			// rank by, and without it neither has anything to sort.
			Values: []string{"HIGH", "MEDIUM", "LOW"},
		}},
		Rules: []entities.DecisionRule{
			{Inputs: []string{"> 10"}, Outputs: []any{"LOW"}},
			{Inputs: []string{"> 40"}, Outputs: []any{"HIGH"}},
			{Inputs: []string{"> 1000"}, Outputs: []any{"MEDIUM"}},
		},
	}
}

func evaluate(t *testing.T, def entities.DecisionDefinition, vars map[string]any) entities.DecisionResult {
	t.Helper()
	result, err := NewDecisionTableEvaluator(NewFEELEvaluator()).EvaluateTable(t.Context(), def, vars)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	return result
}

// TestPriorityRanksByOutputValue is 2.3.3. PRIORITY was an alias for FIRST, so
// a table asking for "the most severe outcome that applies" received "the first
// one written down" — here, LOW instead of HIGH.
func TestPriorityRanksByOutputValue(t *testing.T) {
	result := evaluate(t, severityTable(entities.HitPolicyPriority, ""), map[string]any{"amount": 50.0})

	if result.Values["severity"] != "HIGH" {
		t.Errorf("severity = %v, want HIGH — PRIORITY ranks by the output value list, not table order",
			result.Values["severity"])
	}
	if len(result.MatchedRules) != 1 {
		t.Errorf("PRIORITY returned %d lines, want exactly one", len(result.MatchedRules))
	}
}

// TestOutputOrderReturnsEveryMatchRanked is half of 2.3.4.
func TestOutputOrderReturnsEveryMatchRanked(t *testing.T) {
	result := evaluate(t, severityTable(entities.HitPolicyOutputOrder, ""), map[string]any{"amount": 50.0})

	got, ok := result.Values["severity"].([]any)
	if !ok {
		t.Fatalf("severity = %v (%T), want a list of every match", result.Values["severity"], result.Values["severity"])
	}
	if len(got) != 2 || got[0] != "HIGH" || got[1] != "LOW" {
		t.Errorf("severity = %v, want [HIGH LOW] — every match, most important first", got)
	}
}

// TestRuleOrderReturnsEveryMatchInTableOrder is the other half of 2.3.4, and
// the contrast that shows the two policies differ.
func TestRuleOrderReturnsEveryMatchInTableOrder(t *testing.T) {
	result := evaluate(t, severityTable(entities.HitPolicyRuleOrder, ""), map[string]any{"amount": 50.0})

	got, ok := result.Values["severity"].([]any)
	if !ok {
		t.Fatalf("severity = %v, want a list", result.Values["severity"])
	}
	if len(got) != 2 || got[0] != "LOW" || got[1] != "HIGH" {
		t.Errorf("severity = %v, want [LOW HIGH] — every match in the order they are written", got)
	}
}

// TestCollectWithoutAggregatorReturnsEveryMatch covers a defect found while
// completing the set: COLLECT ran through a builder that writes one value per
// column, so each matching line overwrote the previous and a table asking for
// all of them received only the last.
func TestCollectWithoutAggregatorReturnsEveryMatch(t *testing.T) {
	result := evaluate(t, severityTable(entities.HitPolicyCollect, ""), map[string]any{"amount": 50.0})

	got, ok := result.Values["severity"].([]any)
	if !ok {
		t.Fatalf("severity = %v (%T), want a list of every match", result.Values["severity"], result.Values["severity"])
	}
	if len(got) != 2 {
		t.Errorf("COLLECT returned %d values, want both matches", len(got))
	}
}

func TestCollectWithAggregatorStillAggregates(t *testing.T) {
	def := entities.DecisionDefinition{
		Key:         "bonus",
		HitPolicy:   entities.HitPolicyCollect,
		Aggregation: entities.AggregationSum,
		Inputs:      []entities.DecisionInput{{ID: "in1", Expression: "amount", Type: "number"}},
		Outputs:     []entities.DecisionOutput{{ID: "out1", Name: "bonus", Type: "number"}},
		Rules: []entities.DecisionRule{
			{Inputs: []string{"> 10"}, Outputs: []any{100}},
			{Inputs: []string{"> 40"}, Outputs: []any{200}},
		},
	}

	result := evaluate(t, def, map[string]any{"amount": 50.0})
	if result.Values["bonus"] != 300.0 {
		t.Errorf("bonus = %v, want 300", result.Values["bonus"])
	}
}

// TestAnyRefusesContradictoryLines pins the corrected ANY semantics. ANY is the
// author asserting that every matching line agrees; when they disagree the
// table contradicts itself, and quietly picking one is how a decision table
// starts lying about what it decided.
func TestAnyRefusesContradictoryLines(t *testing.T) {
	def := severityTable(entities.HitPolicyAny, "")

	_, err := NewDecisionTableEvaluator(NewFEELEvaluator()).
		EvaluateTable(t.Context(), def, map[string]any{"amount": 50.0})
	if err == nil {
		t.Fatal("ANY accepted two lines that disagree; it must refuse")
	}
	if !strings.Contains(err.Error(), "disagree") {
		t.Errorf("error = %q, want it to say the lines disagree", err)
	}

	// Agreeing lines are fine.
	def.Rules = []entities.DecisionRule{
		{Inputs: []string{"> 10"}, Outputs: []any{"HIGH"}},
		{Inputs: []string{"> 40"}, Outputs: []any{"HIGH"}},
	}
	if result := evaluate(t, def, map[string]any{"amount": 50.0}); result.Values["severity"] != "HIGH" {
		t.Errorf("severity = %v, want HIGH when every matching line agrees", result.Values["severity"])
	}
}

func TestUniqueRefusesOverlap(t *testing.T) {
	_, err := NewDecisionTableEvaluator(NewFEELEvaluator()).
		EvaluateTable(t.Context(), severityTable(entities.HitPolicyUnique, ""), map[string]any{"amount": 50.0})
	if err == nil {
		t.Fatal("UNIQUE accepted two matching lines")
	}
	// The message names the rows an author sees, not array offsets.
	if !strings.Contains(err.Error(), "1") || !strings.Contains(err.Error(), "2") {
		t.Errorf("error = %q, want it to name the overlapping lines", err)
	}
}

// TestPriorityNeedsItsValueList makes the missing-configuration case explicit
// rather than silently degrading to table order, which is what PRIORITY did
// for its whole previous life.
func TestPriorityNeedsItsValueList(t *testing.T) {
	def := severityTable(entities.HitPolicyPriority, "")
	def.Outputs[0].Values = nil

	_, err := NewDecisionTableEvaluator(NewFEELEvaluator()).
		EvaluateTable(t.Context(), def, map[string]any{"amount": 50.0})
	if err == nil {
		t.Fatal("PRIORITY ran with no output value list; there is nothing to rank by")
	}
	if !strings.Contains(err.Error(), "priority order") {
		t.Errorf("error = %q, want it to say what is missing", err)
	}
}

// TestUnlistedOutputSortsLast keeps an authoring gap from stalling an instance:
// a value missing from the priority list ranks last rather than failing.
func TestUnlistedOutputSortsLast(t *testing.T) {
	def := severityTable(entities.HitPolicyOutputOrder, "")
	def.Rules = append(def.Rules, entities.DecisionRule{Inputs: []string{"> 10"}, Outputs: []any{"UNLISTED"}})

	result := evaluate(t, def, map[string]any{"amount": 50.0})
	got := result.Values["severity"].([]any)
	if got[len(got)-1] != "UNLISTED" {
		t.Errorf("severity = %v, want the unlisted value last", got)
	}
}

func TestUnknownHitPolicyIsRefused(t *testing.T) {
	_, err := NewDecisionTableEvaluator(NewFEELEvaluator()).
		EvaluateTable(t.Context(), severityTable("NONSENSE", ""), map[string]any{"amount": 50.0})
	if err == nil {
		t.Fatal("an unknown hit policy was accepted; it used to fall through to FIRST")
	}
}

func TestNoMatchIsAnEmptyResultNotAnError(t *testing.T) {
	result := evaluate(t, severityTable(entities.HitPolicyPriority, ""), map[string]any{"amount": 1.0})
	if len(result.MatchedRules) != 0 {
		t.Errorf("matched %v, want nothing", result.MatchedRules)
	}
}
