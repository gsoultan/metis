package impl

import (
	"context"
	"errors"
	"fmt"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic"
)

// errIterationNotWaiting refuses a completion for a run of a step that is not
// waiting: it has already been counted, or it was withdrawn when the step
// finished without it. Counting it anyway is how a step finishes one approval
// early.
//
// Its text reaches whoever completed a task, so it names no step and no id.
//
// Whether a run is waiting is ProcessInstance.WaitsFor, here and for every
// caller that is not a person: a job asks before its call and again before it
// uses the result (tokenWaitsAt), a worker's report is asked before it is
// accepted (externalTaskService.withdrawnBecauseStepEnded), a called process
// before it resumes its parent (EndEventHandler.resumeParent) — and each does
// something other than arrive here when the answer is no. What can still be
// refused is a completion that reaches the engine through a step the asker did
// not ask about: work inside a sub-process whose run was ended from outside
// it, which finishes its own step and then finds the sub-process around it
// over.
var errIterationNotWaiting = apierr.Invalidf(
	"the process is no longer waiting for this part of the step, so it cannot be completed again")

// errStepAlreadyFinished refuses a completion on a repeating step that is not
// counting runs and holds no run of its own: the step finished earlier and the
// process has moved on. Advancing again would run everything after it twice.
var errStepAlreadyFinished = apierr.Invalidf(
	"this step has already finished and the process has moved on, so there is nothing left to complete")

// isEngineRefusal reports whether err is the engine declining a completion
// for a step that is not waiting for it. That is a statement about the
// process, not a failure of the work that was done, and nothing that handles
// failures of work — an error boundary event — is the place for it.
func isEngineRefusal(err error) bool {
	return errors.Is(err, errIterationNotWaiting) || errors.Is(err, errStepAlreadyFinished)
}

// removeOrCheckMultiInstance handles token removal for both simple and multi-instance
// nodes.  Returns (true, nil) when execution should continue past the node.
func (e *Engine) removeOrCheckMultiInstance(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node *entities.Node, nodeID, iterationID string) (bool, error) {
	if !node.Repeats() {
		// By node ID, not by node: this branch deliberately accepts a nil node —
		// the definition no longer describes what the token is sitting on — and
		// the token still has to come off, or the instance keeps a token nothing
		// will ever advance.
		instance.RemoveTokenByNodeID(nodeID)
		return true, nil
	}
	return e.checkMultiInstanceCompletion(ctx, instance, def, node, nodeID, iterationID)
}

// checkMultiInstanceCompletion retires one finished iteration and returns
// (true, nil) when the step is done.
//
// One completion, one token. It used to count the completion and remove the
// token of whichever iteration it was told about — and a task told it about
// none, so the count rose while every token stayed. The process moved on after
// the last approval and the instance could never end, because an end event
// finishes an instance only when no token is left.
//
// So a completion for a run the step is not waiting for is refused rather than
// counted (ProcessInstance.WaitsFor), the run's token is retired with its
// count, and a finished step gives up everything it still holds: its tokens
// always, and — when its completion condition ended it early — the tasks of
// the iterations nobody will finish, and the work parked for workers on their
// behalf (endActivity).
func (e *Engine) checkMultiInstanceCompletion(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node *entities.Node, nodeID, iterationID string) (bool, error) {
	counting := instance.IsMultiInstanceActive(nodeID)
	if !instance.WaitsFor(node, iterationID) {
		if counting {
			return false, errIterationNotWaiting
		}
		return false, errStepAlreadyFinished
	}
	if !counting {
		// The step given nothing to repeat over: it runs once, on a plain
		// token, and finishes like any other step. Its completion condition is
		// not asked — the counters it is written against describe iterations,
		// and there are none.
		instance.RemoveTokenByIteration(node, "")
		return true, nil
	}

	// A run's token is retired with its count. The one run that has none is a
	// run of a sub-process entered before sub-processes kept their tokens (see
	// WaitsFor): it is counted, and there is nothing to retire.
	if iteration, held := instance.WaitingIteration(node, iterationID); held {
		instance.RemoveTokenByIteration(node, iteration)
	}
	completed, total := instance.CompleteMultiInstanceIteration(nodeID)

	if !multiInstanceDone(instance, node, completed, total) {
		return false, e.continueMultiInstance(ctx, instance, def, node, completed, total)
	}

	if completed < total {
		// Ended by its completion condition with iterations still open. BPMN
		// 2.0.2 §10.3.8: the condition "cancels the remaining Activity
		// instances" — their tokens, and the tasks behind them. Leaving the
		// tasks open is what let a later completion advance the process a
		// second time.
		return true, e.endActivity(ctx, instance, node)
	}

	// Every iteration finished, so no task is open and nothing is read. A token
	// may still be here: one that a completion made before migration 31 counted
	// and did not retire.
	instance.RemoveTokenByNode(node)
	instance.FinishMultiInstance(nodeID)
	return true, nil
}

// multiInstanceDone reports whether a step that has finished `completed` of
// `total` iterations is done: every iteration has finished, or its completion
// condition holds — whichever comes first (BPMN 2.0.2 §13.2.7).
//
// The condition used to replace "every iteration has finished" rather than add
// to it. "Two of them" over a list of one never read true, and the step waited
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
