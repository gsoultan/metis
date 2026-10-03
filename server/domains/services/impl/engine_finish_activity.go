package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// The engine is asked whether it is a finisher where it is used (newNodeActions),
// so a method that stopped matching the interface would not fail to compile
// there: it would quietly stop being used. This is where it fails instead.
var _ servicecontracts.ActivityFinisher = (*Engine)(nil)

// FinishActivity ends a step whatever its loop, and moves the instance on
// from it once.
//
// BPMN 2.0.2 §10.3.8 and §13.2.7 say what a repeating step does when its
// completion condition becomes true: "the remaining instances are canceled"
// and the step is left by one token. This is that behaviour taken
// unconditionally — the step is over because somebody with the authority said
// so, not because its condition read true. Every token on the step goes, a
// repeating approval stops counting its runs, and the tasks still open are
// withdrawn and announced (endActivity); then the instance does what it does
// after any finished step (continuePastFinished).
//
// Proceed cannot do this for a step that repeats: it counts one run and
// leaves the rest, and starts the next run of a sequential one. For a step
// that does not repeat the two are the same.
//
// Whether the step should be ended is the caller's question, and the instance
// is the row the caller locked.
func (e *Engine) FinishActivity(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string) error {
	return e.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		node := def.FindNode(nodeID)
		if node == nil {
			// The caller's mistake, never the client's: both callers have
			// already found the step in this definition. A plain error, so it
			// surfaces as a server error and is logged.
			return fmt.Errorf("finishing a step: the process that instance %s runs has no step %q", instance.ID, nodeID)
		}
		if err := e.endActivity(txCtx, instance, node); err != nil {
			return err
		}
		return e.continuePastFinished(txCtx, instance, def, node, nodeID)
	})
}

// continuePastFinished is what follows once an activity is done, however it
// became done: its last run was completed, its completion condition was met,
// or it was ended whole (FinishActivity). The step's own tokens are already
// gone when this is called.
//
// The order is the advance's, and both ways of finishing share it so that a
// step ended whole leaves the instance exactly where the same step, completed,
// would have left it.
func (e *Engine) continuePastFinished(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node *entities.Node, nodeID string) error {
	// A step inside an ad-hoc sub-process finishing is what makes its completion
	// condition worth re-reading: the condition is written against the work done
	// inside, so it can only become true here.
	if done, err := e.checkAdHocCompletion(ctx, instance, def, node); err != nil || done {
		return err
	}

	// Step 3: clean up sibling tokens and subscriptions.
	if err := e.cleanupEventBasedGatewaySiblings(ctx, instance, def, nodeID); err != nil {
		return err
	}
	if err := e.cleanupSubscriptions(ctx, instance, nodeID, def); err != nil {
		return err
	}

	// Step 4: mark node completed and follow outgoing flows.
	instance.MarkCompleted(node)
	if err := e.followOutgoingFlows(ctx, instance, def, nodeID); err != nil {
		return err
	}

	// Step 5: a conditional event waiting elsewhere in this instance may now be
	// satisfied by what this advance changed.
	return e.resumeSatisfiedConditionalEvents(ctx, instance, def)
}
