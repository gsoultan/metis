package impl

import (
	"context"
	"fmt"

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
// If it is not satisfied, the subprocess would have to wait for a knowledge
// worker to activate the tasks inside it. Nothing in this repository can do
// that: the AdHocActivator contract that describes it has no implementation and
// no caller. So rather than parking a token and reporting success, this reports
// that the instance has no route forward. A process that can never continue is
// an incident; silently hanging one leaves nothing to investigate.
func (h *AdHocSubProcessHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	if !h.isCompletionConditionMet(&node, instance) {
		return fmt.Errorf(
			"ad-hoc sub-process %q cannot be advanced: its completion condition %q is not satisfied and no task activation path is implemented (AdHocActivator has no implementation), so the instance would wait forever",
			node.ID, node.CompletionCondition)
	}

	instance.AddTokenWithIteration(&node, iterationID)
	return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
}

// isCompletionConditionMet evaluates the node's CompletionCondition with the
// same chain gateways use for sequence-flow conditions, so an ad-hoc condition
// is written the same way as every other process condition ("js:done >= 2",
// "status=approved", or a plain boolean variable name).
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
