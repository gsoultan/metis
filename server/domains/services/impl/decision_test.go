package impl

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

func TestDecisionService_FullDMN(t *testing.T) {
	ctx := context.Background()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	svc := NewDecisionService(repo, NewDecisionTableEvaluator(NewFEELEvaluator()))

	projectID, _ := uuid.NewV7()

	t.Run("HitPolicy UNIQUE", func(t *testing.T) {
		d := entities.DecisionDefinition{
			Project:   &entities.Project{ID: projectID},
			Key:       "unique-decision",
			HitPolicy: entities.HitPolicyUnique,
			Inputs:    []entities.DecisionInput{{ID: "in1", Expression: "score", Type: "number"}},
			Outputs:   []entities.DecisionOutput{{ID: "out1", Name: "result", Type: "string"}},
			Rules: []entities.DecisionRule{
				{Inputs: []string{"> 80"}, Outputs: []any{"Excellent"}},
				{Inputs: []string{"<= 80"}, Outputs: []any{"Good"}},
			},
		}
		_, _ = svc.CreateDecision(ctx, d)

		res, err := svc.Evaluate(ctx, "unique-decision", 0, map[string]any{"score": 90})
		if err != nil {
			t.Fatalf("Evaluate failed: %v", err)
		}
		if res.Values["result"] != "Excellent" {
			t.Errorf("Expected Excellent, got %v", res.Values["result"])
		}

		// Multiple matches should fail
		d2 := entities.DecisionDefinition{
			Project:   &entities.Project{ID: projectID},
			Key:       "unique-fail",
			HitPolicy: entities.HitPolicyUnique,
			Inputs:    []entities.DecisionInput{{ID: "in1", Expression: "score", Type: "number"}},
			Outputs:   []entities.DecisionOutput{{ID: "out1", Name: "result", Type: "string"}},
			Rules: []entities.DecisionRule{
				{Inputs: []string{"> 50"}, Outputs: []any{"Pass"}},
				{Inputs: []string{"> 80"}, Outputs: []any{"Excellent"}},
			},
		}
		_, _ = svc.CreateDecision(ctx, d2)
		_, err = svc.Evaluate(ctx, "unique-fail", 0, map[string]any{"score": 90})
		if err == nil {
			t.Fatal("Expected error for UNIQUE hit policy with multiple matches")
		}
	})

	t.Run("HitPolicy COLLECT with Aggregation SUM", func(t *testing.T) {
		d := entities.DecisionDefinition{
			Project:     &entities.Project{ID: projectID},
			Key:         "collect-sum",
			HitPolicy:   entities.HitPolicyCollect,
			Aggregation: entities.AggregationSum,
			Inputs:      []entities.DecisionInput{{ID: "in1", Expression: "val", Type: "number"}},
			Outputs:     []entities.DecisionOutput{{ID: "out1", Name: "bonus", Type: "number"}},
			Rules: []entities.DecisionRule{
				{Inputs: []string{"> 10"}, Outputs: []any{100}},
				{Inputs: []string{"> 20"}, Outputs: []any{200}},
			},
		}
		_, _ = svc.CreateDecision(ctx, d)

		res, err := svc.Evaluate(ctx, "collect-sum", 0, map[string]any{"val": 25})
		if err != nil {
			t.Fatalf("Evaluate failed: %v", err)
		}
		if res.Values["bonus"] != 300.0 {
			t.Errorf("Expected 300, got %v", res.Values["bonus"])
		}
	})

	t.Run("Decision Requirements (DRD)", func(t *testing.T) {
		// Parent decision
		d1 := entities.DecisionDefinition{
			Project: &entities.Project{ID: projectID},
			Key:     "base-price",
			Inputs:  []entities.DecisionInput{{ID: "in1", Expression: "category", Type: "string"}},
			Outputs: []entities.DecisionOutput{{ID: "out1", Name: "price", Type: "number"}},
			Rules: []entities.DecisionRule{
				{Inputs: []string{"A"}, Outputs: []any{100}},
				{Inputs: []string{"B"}, Outputs: []any{200}},
			},
		}
		_, _ = svc.CreateDecision(ctx, d1)

		// Dependent decision
		d2 := entities.DecisionDefinition{
			Project:           &entities.Project{ID: projectID},
			Key:               "final-price",
			RequiredDecisions: []string{"base-price"},
			Inputs:            []entities.DecisionInput{{ID: "in1", Expression: "base-price", Type: "number"}},
			Outputs:           []entities.DecisionOutput{{ID: "out1", Name: "final", Type: "number"}},
			Rules: []entities.DecisionRule{
				{Inputs: []string{"> 150"}, Outputs: []any{180}}, // discount for B
				{Inputs: []string{"<= 150"}, Outputs: []any{100}},
			},
		}
		_, _ = svc.CreateDecision(ctx, d2)

		res, err := svc.Evaluate(ctx, "final-price", 0, map[string]any{"category": "B"})
		if err != nil {
			t.Fatalf("Evaluate failed: %v", err)
		}
		if res.Values["final"] != 180.0 {
			t.Errorf("Expected 180, got %v", res.Values["final"])
		}
	})

	t.Run("FEEL Improvements: Lists", func(t *testing.T) {
		d := entities.DecisionDefinition{
			Project: &entities.Project{ID: projectID},
			Key:     "list-test",
			Inputs:  []entities.DecisionInput{{ID: "in1", Expression: "status", Type: "string"}},
			Outputs: []entities.DecisionOutput{{ID: "out1", Name: "ok", Type: "boolean"}},
			Rules: []entities.DecisionRule{
				{Inputs: []string{"OPEN, IN_PROGRESS"}, Outputs: []any{true}},
				{Inputs: []string{"CLOSED"}, Outputs: []any{false}},
			},
		}
		_, _ = svc.CreateDecision(ctx, d)

		res, _ := svc.Evaluate(ctx, "list-test", 0, map[string]any{"status": "IN_PROGRESS"})
		if res.Values["ok"] != true {
			t.Errorf("Expected true for IN_PROGRESS in [OPEN, IN_PROGRESS]")
		}
	})
}
