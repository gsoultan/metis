package decision_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

func TestDecisionEvaluation(t *testing.T) {
	db := testutils.SetupTestDB(t)

	repo := repositories.NewRepository(db)
	svc := impl.NewDecisionService(repo, impl.NewDecisionTableEvaluator(impl.NewFEELEvaluator()))

	id, _ := uuid.NewV7()
	// CreateAuditEntry a sample decision table
	decision := entities.DecisionDefinition{
		ID:      id,
		Key:     "discount-decision",
		Name:    "Discount Decision",
		Version: 1,
		Inputs: []entities.DecisionInput{
			{ID: "type", Label: "Customer Type", Expression: "customerType", Type: "string"},
			{ID: "amount", Label: "Order Amount", Expression: "orderAmount", Type: "number"},
		},
		Outputs: []entities.DecisionOutput{
			{ID: "discount", Label: "Discount", Name: "discount", Type: "number"},
		},
		Rules: []entities.DecisionRule{
			{
				ID:      "rule1",
				Inputs:  []string{"'VIP'", "> 1000"},
				Outputs: []any{20},
			},
			{
				ID:      "rule2",
				Inputs:  []string{"'VIP'", "<= 1000"},
				Outputs: []any{10},
			},
			{
				ID:      "rule3",
				Inputs:  []string{"'Regular'", "-"},
				Outputs: []any{5},
			},
		},
	}

	_, err := svc.CreateDecision(context.Background(), decision)
	if err != nil {
		t.Fatalf("failed to create decision: %v", err)
	}

	tests := []struct {
		name      string
		variables map[string]any
		expected  float64
	}{
		{
			name: "VIP with high amount",
			variables: map[string]any{
				"customerType": "VIP",
				"orderAmount":  1500,
			},
			expected: 20,
		},
		{
			name: "VIP with low amount",
			variables: map[string]any{
				"customerType": "VIP",
				"orderAmount":  500,
			},
			expected: 10,
		},
		{
			name: "Regular customer",
			variables: map[string]any{
				"customerType": "Regular",
				"orderAmount":  500,
			},
			expected: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.Evaluate(t.Context(), "discount-decision", 1, tt.variables)
			if err != nil {
				t.Fatalf("evaluation failed: %v", err)
			}

			discount, ok := result.Values["discount"].(float64)
			if !ok {
				// goja might return int as int64 or float64 depending on how it's handled
				// But since we provided int in rule, it might be float64 after unmarshal from JSON
				// actually we are using entities directly here.
				// Wait, the entities.DecisionRule has []any for outputs.
				// In the test we set them as int.
				if dInt, ok := result.Values["discount"].(int); ok {
					discount = float64(dInt)
				} else {
					t.Fatalf("expected discount to be a number, got %T", result.Values["discount"])
				}
			}

			if discount != tt.expected {
				t.Errorf("expected discount %v, got %v", tt.expected, discount)
			}
		})
	}
}
