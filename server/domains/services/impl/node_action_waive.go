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
func (a nodeActions) waive(
	ctx context.Context,
	live *entities.ProcessInstance,
	def *entities.ProcessDefinition,
	nodeID string,
) ([]models.TaskModel, error) {
	withdrawn, err := a.withdrawOn(ctx, live.ID, nodeID)
	if err != nil {
		return nil, err
	}
	if err := a.engine.Proceed(ctx, live, def, nodeID); err != nil {
		return nil, fmt.Errorf("advancing instance %s past %q: %w", live.ID, nodeID, err)
	}
	return withdrawn, nil
}
