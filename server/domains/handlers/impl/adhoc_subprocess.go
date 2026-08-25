package impl

import (
	"context"

	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/domains/logic"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
)

// AdHocSubProcessHandler handles ad-hoc subprocesses where tasks can be activated
// in any order, any number of times, until the completion condition is satisfied.
// It implements the Strategy pattern — the SubProcessHandler delegates to this
// handler when node.IsAdHoc is true.
type AdHocSubProcessHandler struct {
	engine servicecontracts.EngineRunner
}

// NewAdHocSubProcessHandler creates a new AdHocSubProcessHandler.
//
// It takes no expression evaluator: completion conditions go through the shared
// condition evaluator chain, the same one gateways use for sequence flows.
func NewAdHocSubProcessHandler(engine servicecontracts.ExecutionEngine) *AdHocSubProcessHandler {
	return &AdHocSubProcessHandler{engine: engine}
}

// DoExecute enters the ad-hoc subprocess and checks its completion condition.
// If the condition is already satisfied — including the case of no condition at
// all — it proceeds.
//
// If it is not satisfied, the sub-process holds a token and waits for a
// knowledge worker to activate the steps inside it, one at a time and in
// whatever order the work needs. Each step that finishes re-checks the
// condition, which is what eventually lets the process through.
func (h *AdHocSubProcessHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	// The token is already on this node: whatever flowed here put it there. The
	// handler used to add a second one, which the proceeding path hid because
	// removing by node id clears both — but waiting kept it, and a node holding
	// two tokens is a node the engine never finishes with.
	if h.isCompletionConditionMet(&node, instance) {
		return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
	}

	// Waiting: the token marks where the process is, and activating a step
	// inside is what eventually moves it on.
	return h.engine.UpdateInstance(ctx, *instance)
}

// isCompletionConditionMet evaluates the node's CompletionCondition with the
// same chain gateways use for sequence-flow conditions, so an ad-hoc condition
// is written the same way as every other process condition ("done >= 2" in
// FEEL, "status=approved", or a plain boolean variable name).
//
// It previously went through the DMN FEEL evaluator, which is a decision-table
// cell matcher: it compares against variables["_input"] and ignores the rest of
// the map. An ad-hoc sub-process never sets _input, so every non-empty condition
// evaluated to false and the only reachable outcomes were "no condition, proceed
// immediately" and "any condition, hang forever".
func (h *AdHocSubProcessHandler) isCompletionConditionMet(node *entities.Node, instance *entities.ProcessInstance) bool {
	if node.CompletionCondition == "" {
		return true
	}
	return logic.GetConditionEvaluatorChain().Evaluate(node.CompletionCondition, instance.Variables)
}
