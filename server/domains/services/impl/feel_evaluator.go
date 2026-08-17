package impl

import (
	"context"

	"github.com/gsoultan/gobpm/server/domains/logic/feel"
)

// FEELEvaluator adapts the FEEL engine to the ExpressionEvaluator contract.
//
// The name is the same as its predecessor, which was not FEEL: that one matched
// strings, so `[1..10]` worked only because it looked for "..", `"a","b"` worked
// only because it split on commas — and therefore a comma inside a range or a
// quoted string broke it — and `1` equalled `"1"` because both were compared as
// printed text. It had no dates, no arithmetic, no and/or, no property paths and
// no functions.
//
// This adapter is thin on purpose. The language lives in
// server/domains/logic/feel, where it is testable on its own and can be reused
// by gateway conditions and input/output mappings without dragging the service
// layer along.
type FEELEvaluator struct{}

// NewFEELEvaluator creates a new FEELEvaluator.
func NewFEELEvaluator() *FEELEvaluator {
	return &FEELEvaluator{}
}

// Evaluate evaluates a full FEEL expression and returns its value as a plain Go
// value, ready to store in process variables.
func (e *FEELEvaluator) Evaluate(_ context.Context, expression string, variables map[string]any) (any, error) {
	value, err := feel.Evaluate(expression, variables)
	if err != nil {
		return nil, err
	}
	return value.ToAny(), nil
}

// EvaluateBool evaluates a DMN decision-table cell against the input the table
// evaluator placed in variables under feel.InputName.
//
// A cell is a unary test, not an expression: `< 100` has nothing on its left,
// `"GOLD","SILVER"` is a disjunction rather than a list, and an empty cell (or
// `-`) matches anything. It gets its own grammar rather than being coerced into
// expression shape, which is what the string matcher had to do.
func (e *FEELEvaluator) EvaluateBool(_ context.Context, expression string, variables map[string]any) (bool, error) {
	input := variables[feel.InputName]
	return feel.EvaluateUnaryTests(expression, input, variables)
}
