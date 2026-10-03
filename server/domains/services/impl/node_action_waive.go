package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// waive withdraws the work parked on one step and advances the instance past
// it as though the step had been performed, and returns the tasks it withdrew
// as they were before.
//
// live is the row its caller locked, and def the graph that instance runs:
// only that graph knows what follows the step. Both writes join the caller's
// unit of work, so an advance that fails — a gateway after the step with no
// branch to take — puts the work back as it was once the caller rolls back.
//
// Work somebody does — a user or a manual task — is ended whole (finishWhole):
// such a step may repeat, and an advance counts one run of it and leaves the
// rest. Every other step is withdrawn from and advanced past as it always
// was, whatever its loop.
func (a nodeActions) waive(
	ctx context.Context,
	live *entities.ProcessInstance,
	def *entities.ProcessDefinition,
	nodeID string,
) ([]models.TaskModel, error) {
	if a.finisher != nil && isWorkSomebodyDoes(def.FindNode(nodeID)) {
		return a.finishWhole(ctx, live, def, nodeID)
	}
	withdrawn, err := a.withdrawOn(ctx, live.ID, nodeID)
	if err != nil {
		return nil, err
	}
	if err := a.engine.Proceed(ctx, live, def, nodeID); err != nil {
		return nil, fmt.Errorf("advancing instance %s past %q: %w", live.ID, nodeID, err)
	}
	return withdrawn, nil
}

// finishWhole ends every run of a step and advances past it once, and returns
// the tasks that were open on it as they were before.
//
// The tasks are read here and withdrawn by the engine: ending an activity is
// the engine's to do, and it tells whoever held each task as it withdraws it.
// Reading first is what lets the record say what was withdrawn — afterwards
// they are cancelled rows among the step's other cancelled rows.
func (a nodeActions) finishWhole(
	ctx context.Context,
	live *entities.ProcessInstance,
	def *entities.ProcessDefinition,
	nodeID string,
) ([]models.TaskModel, error) {
	withdrawn, err := a.openOn(ctx, live.ID, nodeID)
	if err != nil {
		return nil, err
	}
	if err := a.finisher.FinishActivity(ctx, live, def, nodeID); err != nil {
		return nil, fmt.Errorf("advancing instance %s past %q: %w", live.ID, nodeID, err)
	}
	return withdrawn, nil
}

// isWorkSomebodyDoes reports whether node is a step a person performs: the
// only kind of step whose runs are counted strictly (Node.IsRepeatingApproval),
// and so the only kind ended whole.
func isWorkSomebodyDoes(node *entities.Node) bool {
	return node != nil && (node.Type == entities.UserTask || node.Type == entities.ManualTask)
}
