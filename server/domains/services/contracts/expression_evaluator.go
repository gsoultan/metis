package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// ExpressionEvaluator is a Strategy interface for evaluating expressions against
// a set of variables. Implementations can support JavaScript, FEEL (DMN), Groovy,
// or any other expression language without changing the calling code.
// Used in sequence flow conditions, completion conditions, and decision rules.
type ExpressionEvaluator interface {
	// Evaluate evaluates the expression string against the given variables and
	// returns the result. The result type depends on the expression (bool, number, string).
	Evaluate(ctx context.Context, expression string, variables map[string]any) (any, error)

	// MatchesCell reports whether a decision-table condition cell accepts the
	// case in front of it: its own column's value, with the decision's
	// variables in scope.
	//
	// It replaces EvaluateBool, which took the column's value inside the
	// variables under a reserved name. That map held nothing else, so a cell
	// could not see another column; and filling it with the decision's
	// variables would have let one of them take the reserved name.
	MatchesCell(ctx context.Context, cell string, scope entities.DecisionCellScope) (bool, error)
}
