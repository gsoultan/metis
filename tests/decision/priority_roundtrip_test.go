package decision_test

import (
	"testing"

	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/tests/testutils"
)

// PRIORITY and OUTPUT ORDER rank matches by the output column's list of allowed
// values, and refuse to evaluate without it. So the list has to survive being
// saved: if it is dropped on the way to the database, every such table works in
// the editor's preview and fails the moment a process calls it.
//
// It very nearly was dropped — the field existed on the entity and on nothing
// else, so the adapter wrote a column definition with no priority order at all.
func TestOutputPriorityOrderSurvivesASave(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()
	repo := repositories.NewRepository(db)
	svc := impl.NewDecisionService(repo, impl.NewDecisionTableEvaluator(impl.NewFEELEvaluator()))

	id, err := svc.CreateDecision(ctx, entities.DecisionDefinition{
		Key:       "severity",
		Name:      "Severity",
		HitPolicy: entities.HitPolicyPriority,
		Inputs:    []entities.DecisionInput{{ID: "in1", Expression: "amount", Type: "number"}},
		Outputs: []entities.DecisionOutput{{
			ID:     "out1",
			Name:   "severity",
			Type:   "string",
			Values: []string{"HIGH", "MEDIUM", "LOW"},
		}},
		Rules: []entities.DecisionRule{
			{Inputs: []string{"> 10"}, Outputs: []any{"LOW"}},
			{Inputs: []string{"> 40"}, Outputs: []any{"HIGH"}},
		},
	})
	if err != nil {
		t.Fatalf("create decision: %v", err)
	}

	stored, err := svc.GetDecision(ctx, id)
	if err != nil {
		t.Fatalf("get decision: %v", err)
	}
	if len(stored.Outputs) != 1 {
		t.Fatalf("outputs = %v, want one column", stored.Outputs)
	}
	got := stored.Outputs[0].Values
	if len(got) != 3 || got[0] != "HIGH" || got[1] != "MEDIUM" || got[2] != "LOW" {
		t.Fatalf("values = %v, want [HIGH MEDIUM LOW] in that order", got)
	}

	// And the table it describes still decides what its author meant: the most
	// severe outcome that applies, not the first one written down.
	result, err := svc.Evaluate(ctx, "severity", 0, map[string]any{"amount": 50.0})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if result.Values["severity"] != "HIGH" {
		t.Errorf("severity = %v, want HIGH", result.Values["severity"])
	}
}
