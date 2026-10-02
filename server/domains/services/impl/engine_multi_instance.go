package impl

import (
	"context"
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
var errIterationNotWaiting = apierr.Invalidf(
	"the process is no longer waiting for this part of the step, so it cannot be completed again")

// errStepAlreadyFinished refuses a completion on a repeating step that is not
// counting runs and holds no run of its own: the step finished earlier and the
// process has moved on. Advancing again would run everything after it twice.
var errStepAlreadyFinished = apierr.Invalidf(
	"this step has already finished and the process has moved on, so there is nothing left to complete")

// removeOrCheckMultiInstance handles token removal for both simple and multi-instance
// nodes.  Returns (true, nil) when execution should continue past the node.
func (e *Engine) removeOrCheckMultiInstance(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node *entities.Node, nodeID, iterationID string) (bool, error) {
	if node == nil || node.MultiInstanceType == "" || node.MultiInstanceType == "none" {
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
// So the iteration is resolved first (ProcessInstance.WaitingIteration), a
// completion with no token to retire is refused rather than counted, and a
// finished step gives up every token it still holds — which also clears one a
// completion made before migration 31 left behind.
func (e *Engine) checkMultiInstanceCompletion(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node *entities.Node, nodeID, iterationID string) (bool, error) {
	if !instance.IsMultiInstanceActive(nodeID) {
		return finishUncountedRun(instance, node, iterationID)
	}

	iteration, waiting := instance.WaitingIteration(node, iterationID)
	if !waiting {
		return false, errIterationNotWaiting
	}
	instance.RemoveTokenByIteration(node, iteration)
	completed, total := instance.CompleteMultiInstanceIteration(nodeID)

	if !multiInstanceDone(instance, node, completed, total) {
		return false, e.continueMultiInstance(ctx, instance, def, node, completed, total)
	}

	instance.RemoveTokenByNode(node)
	instance.FinishMultiInstance(nodeID)
	return true, nil
}

// finishUncountedRun finishes a repeating step that is not counting runs.
//
// That is the step given nothing to repeat over: it runs once, on a plain
// token, and finishes like any other step. Its completion condition is not
// asked — the counters it is written against describe iterations, and there
// are none.
//
// Anything else is a completion for a step that already finished.
func finishUncountedRun(instance *entities.ProcessInstance, node *entities.Node, iterationID string) (bool, error) {
	if iterationID != "" || !instance.HasPlainToken(node) {
		return false, errStepAlreadyFinished
	}
	instance.RemoveTokenByIteration(node, "")
	return true, nil
}

// multiInstanceDone reports whether a step that has finished `completed` of
// `total` iterations is done.
func multiInstanceDone(instance *entities.ProcessInstance, node *entities.Node, completed, total int) bool {
	if node.CompletionCondition == "" {
		return completed >= total
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
