package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/gobpm/server/domains/entities"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
)

// AdHocSubProcessHandler handles ad-hoc subprocesses where tasks can be activated
// in any order, any number of times, until the completion condition is satisfied.
// It implements the Strategy pattern — the SubProcessHandler delegates to this
// handler when node.IsAdHoc is true.
type AdHocSubProcessHandler struct {
	engine   servicecontracts.EngineRunner
	exprEval servicecontracts.ExpressionEvaluator
}

// NewAdHocSubProcessHandler creates a new AdHocSubProcessHandler.
func NewAdHocSubProcessHandler(engine servicecontracts.ExecutionEngine, exprEval servicecontracts.ExpressionEvaluator) *AdHocSubProcessHandler {
	return &AdHocSubProcessHandler{engine: engine, exprEval: exprEval}
}

// DoExecute enters the ad-hoc subprocess by placing a token on it and
// immediately checking the completion condition. If the condition is already
// satisfied (e.g., empty/wildcard), it proceeds; otherwise it waits for
// explicit task activations from the knowledge worker.
func (h *AdHocSubProcessHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	instance.AddTokenWithIteration(&node, iterationID)

	complete, err := h.isCompletionConditionMet(ctx, node, instance)
	if err != nil {
		return fmt.Errorf("ad-hoc subprocess %s: evaluate completion condition: %w", node.ID, err)
	}
	if !complete {
		// Wait for explicit task activations — no auto-proceed.
		return h.engine.UpdateInstance(ctx, *instance)
	}
	return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
}

// isCompletionConditionMet evaluates the node's CompletionCondition expression.
// Returns true (complete) when no condition is set or the expression evaluates to true.
func (h *AdHocSubProcessHandler) isCompletionConditionMet(ctx context.Context, node entities.Node, instance *entities.ProcessInstance) (bool, error) {
	if node.CompletionCondition == "" {
		return true, nil
	}
	result, err := h.exprEval.EvaluateBool(ctx, node.CompletionCondition, instance.Variables)
	if err != nil {
		return false, err
	}
	return result, nil
}
