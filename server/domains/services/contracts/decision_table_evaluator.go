package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// DecisionTableEvaluator is a Strategy interface for evaluating a DMN decision
// table against a set of input variables. It applies the table's hit policy
// (UNIQUE, FIRST, COLLECT, etc.) and returns matched output values.
// Decouple from DecisionService so the evaluation algorithm is swappable
// (e.g., basic rule matching vs. full FEEL engine).
type DecisionTableEvaluator interface {
	EvaluateTable(ctx context.Context, def entities.DecisionDefinition, variables map[string]any) (entities.DecisionResult, error)
}
