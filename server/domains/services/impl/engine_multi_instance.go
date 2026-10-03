package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic"
)

// errIterationNotWaiting refuses a completion for a run of a repeating
// approval that is not waiting: it has already been counted, or it was
// withdrawn when the step finished without it. Counting it anyway is how a
// step finishes one approval early.
//
// Its text reaches whoever completed a task, so it names no step and no id.
var errIterationNotWaiting = apierr.Invalidf(
	"the process is no longer waiting for this part of the step, so it cannot be completed again")

// errStepAlreadyFinished refuses a completion on a repeating approval that is
// not counting runs and holds no run of its own: the step finished earlier and
// the process has moved on. Advancing again would run everything after it
// twice.
var errStepAlreadyFinished = apierr.Invalidf(
	"this step has already finished and the process has moved on, so there is nothing left to complete")

// removeOrCheckMultiInstance handles token removal for both simple and multi-instance
// nodes.  Returns (true, nil) when execution should continue past the node.
//
// A step that repeats is counted one of two ways. An approval — a user task
// or a manual task — is counted strictly (checkApprovalCompletion). Every
// other repeating step is counted as it always was
// (checkMultiInstanceCompletion): see Node.IsRepeatingApproval for why the
// strict rule stops at approvals.
func (e *Engine) removeOrCheckMultiInstance(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node *entities.Node, nodeID, iterationID string) (bool, error) {
	if !node.Repeats() {
		// By node ID, not by node: this branch deliberately accepts a nil node —
		// the definition no longer describes what the token is sitting on — and
		// the token still has to come off, or the instance keeps a token nothing
		// will ever advance.
		instance.RemoveTokenByNodeID(nodeID)
		return true, nil
	}
	if node.IsRepeatingApproval() {
		return e.checkApprovalCompletion(ctx, instance, def, node, iterationID)
	}
	return e.checkMultiInstanceCompletion(ctx, instance, def, node, nodeID, iterationID)
}

// checkMultiInstanceCompletion increments the completion counter and returns
// (true, nil) when all iterations are done (or the completion condition is met).
//
// This is the counting every repeating step had before approvals were given
// their own, and it is deliberately left as it was, looseness included: the
// count rises whether or not a token came off, a completion condition replaces
// "every iteration has finished" rather than adding to it, and a step that
// finishes keeps whatever tokens were not named to it. Tightening it is not a
// matter of applying the approval rule here — the runs of a repeating
// sub-process share the tokens of the steps inside it, and that rule refuses
// them advances they are owed. It waits for each run to have tokens of its
// own.
func (e *Engine) checkMultiInstanceCompletion(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node *entities.Node, nodeID, iterationID string) (bool, error) {
	completed, total := instance.CompleteMultiInstanceIteration(nodeID)
	instance.RemoveTokenByIteration(node, iterationID)

	conditionMet := completed >= total
	if node.CompletionCondition != "" {
		// The condition sees the business variables plus BPMN's own progress
		// counters, without either being written back to the instance.
		conditionMet = logic.GetConditionEvaluatorChain().
			Evaluate(node.CompletionCondition, instance.MultiInstanceConditionScope(nodeID))
	}

	if !conditionMet {
		return false, e.continueMultiInstance(ctx, instance, def, node, completed, total)
	}

	// Every iteration is done, so the bookkeeping goes.
	instance.FinishMultiInstance(nodeID)
	return true, nil
}

// checkApprovalCompletion retires one finished run of a repeating approval and
// returns (true, nil) when the step is done.
//
// One completion, one token. The step used to be counted like every other
// repeating step: the count rose and the token of whichever run the completion
// named came off — and a task named none, so the count rose while every token
// stayed. The process moved on after the last approval and the instance could
// never end, because an end event finishes an instance only when no token is
// left.
//
// So a completion for a run the step is not waiting for is refused rather than
// counted (ProcessInstance.AwaitsRun), the run's token is retired with its
// count, and a finished step gives up everything it still holds: its tokens
// always, and — when its completion condition ended it early — the tasks of
// the runs nobody will finish (endActivity).
func (e *Engine) checkApprovalCompletion(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node *entities.Node, iterationID string) (bool, error) {
	counting := instance.IsMultiInstanceActive(node.ID)
	if !instance.AwaitsRun(node, iterationID) {
		if counting {
			return false, errIterationNotWaiting
		}
		return false, errStepAlreadyFinished
	}
	if !counting {
		// The step given nothing to repeat over: it runs once, on a plain
		// token, and finishes like any other step. Its completion condition is
		// not asked — the counters it is written against describe runs, and
		// there are none.
		instance.RemoveTokenByIteration(node, "")
		return true, nil
	}

	iteration, _ := instance.WaitingIteration(node, iterationID)
	instance.RemoveTokenByIteration(node, iteration)
	completed, total := instance.CompleteMultiInstanceIteration(node.ID)

	if !multiInstanceDone(instance, node, completed, total) {
		return false, e.continueMultiInstance(ctx, instance, def, node, completed, total)
	}

	if completed < total {
		// Ended by its completion condition with runs still open. BPMN 2.0.2
		// §10.3.8: the condition "cancels the remaining Activity instances" —
		// their tokens, and the tasks behind them. Leaving the tasks open is
		// what let a later completion advance the process a second time.
		return true, e.endActivity(ctx, instance, node)
	}

	// Every run finished, so no task is open and nothing is read. A token may
	// still be here: one that a completion made before migration 31 counted
	// and did not retire.
	instance.RemoveTokenByNode(node)
	instance.FinishMultiInstance(node.ID)
	return true, nil
}

// multiInstanceDone reports whether a repeating approval that has finished
// `completed` of `total` runs is done: every run has finished, or its
// completion condition holds — whichever comes first (BPMN 2.0.2 §13.2.7).
//
// The condition used to replace "every run has finished" rather than add to
// it. "Two of them" over a list of one never read true, and the step waited
// for an approval nobody had been asked for.
func multiInstanceDone(instance *entities.ProcessInstance, node *entities.Node, completed, total int) bool {
	if completed >= total {
		return true
	}
	if node.CompletionCondition == "" {
		return false
	}
	// The condition sees the business variables plus BPMN's own progress
	// counters, without either being written back to the instance.
	return logic.GetConditionEvaluatorChain().
		Evaluate(node.CompletionCondition, instance.MultiInstanceConditionScope(node.ID))
}

// continueMultiInstance saves the progress of a step that is not done, and
// starts the next iteration of a sequential one.
func (e *Engine) continueMultiInstance(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node *entities.Node, completed, total int) error {
	if err := e.UpdateInstance(ctx, *instance); err != nil {
		return err
	}
	// Sequential means one at a time: the next iteration is started when
	// this one finishes, and nothing was starting it. A task set to run
	// once per supplier ran for the first supplier and the process then sat
	// there, with no error and no task, looking like it was still working.
	if node.MultiInstanceType == "sequential" && completed < total {
		return e.startNextSequentialIteration(ctx, instance, def, node, completed)
	}
	return nil
}

// startNextSequentialIteration runs iteration `index` of a sequential
// multi-instance node.
//
// Each iteration is started from within the one before it, so a collection of
// n runs n deep. The engine's execution-depth bound applies, which is the same
// protection an accidental loop gets: a very long collection is reported as
// exceeding it rather than exhausting the stack.
func (e *Engine) startNextSequentialIteration(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node *entities.Node, index int) error {
	iterationID := fmt.Sprintf("%d", index)
	instance.AddTokenWithIteration(node, iterationID)

	collection, _ := entities.MultiInstanceCollection(instance, *node)
	entities.BindMultiInstanceElement(instance, *node, collection, index)

	if err := e.UpdateInstance(ctx, *instance); err != nil {
		return err
	}
	return e.ExecuteNodeIteration(ctx, instance, def, node.ID, iterationID)
}
