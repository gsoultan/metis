package impl

import (
	"context"
	"fmt"
	"strings"

	"github.com/gsoultan/metis/server/domains/entities"
)

// computeOutputs evaluates the computed output cells of the lines the hit
// policy will read, so the policy itself only ever sees values.
//
// Values, not expressions, because PRIORITY ranks by an output's value, ANY
// compares them and COLLECT sums them: each needs the answer, not the text
// that produces it. Under FIRST, and an unset policy that reads as FIRST, only
// the first line is read, so only its cells are evaluated: a line that matched
// but is never chosen cannot fail the decision.
//
// A cell that cannot be evaluated is an error naming the line and the column.
// Answering null would let the process carry on with a discount of nothing,
// which is a wrong decision with nothing in the timeline to say so.
func (e *DecisionTableEvaluatorImpl) computeOutputs(
	ctx context.Context,
	def entities.DecisionDefinition,
	matched []matchedRule,
	variables map[string]any,
) ([]matchedRule, error) {
	if len(matched) == 0 || !hasComputedOutput(def) {
		return matched, nil
	}
	if def.HitPolicy == entities.HitPolicyFirst || def.HitPolicy == "" {
		matched = matched[:1]
	}
	computed := make([]matchedRule, len(matched))
	for i, m := range matched {
		outputs, err := e.computeLine(ctx, def, m, variables)
		if err != nil {
			return nil, err
		}
		m.rule.Outputs = outputs
		computed[i] = m
	}
	return computed, nil
}

// computeLine returns one line's outputs with its computed cells evaluated. The
// line's own slice is copied, never written: it belongs to the table, which is
// shared by every evaluation of this version.
func (e *DecisionTableEvaluatorImpl) computeLine(
	ctx context.Context,
	def entities.DecisionDefinition,
	m matchedRule,
	variables map[string]any,
) ([]any, error) {
	outputs := append([]any(nil), m.rule.Outputs...)
	for i, column := range def.Outputs {
		if !column.Computed || i >= len(outputs) {
			continue
		}
		expression, isText := outputs[i].(string)
		if !isText {
			// A number or a flag stored in a computed column is already a value.
			continue
		}
		if strings.TrimSpace(expression) == "" {
			outputs[i] = nil
			continue
		}
		value, err := e.expr.Evaluate(ctx, expression, variables)
		if err != nil {
			return nil, fmt.Errorf("line %d could not compute %q from %q: %w", m.index+1, column.Name, expression, err)
		}
		outputs[i] = value
	}
	return outputs, nil
}

func hasComputedOutput(def entities.DecisionDefinition) bool {
	for _, column := range def.Outputs {
		if column.Computed {
			return true
		}
	}
	return false
}
