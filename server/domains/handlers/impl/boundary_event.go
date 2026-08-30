package impl

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// BoundaryEventHandler handles the execution of boundary events when they are triggered.
type BoundaryEventHandler struct {
	engine servicecontracts.EngineRunner
}

func (h *BoundaryEventHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	// If it's interrupting, remove token from the attached node.
	if node.CancelActivity && !node.IsNonInterrupting() {
		attachedNode := def.FindNode(node.AttachedToRef)
		if attachedNode != nil {
			instance.RemoveTokenByNode(attachedNode)
		}
	}

	// Boundary events always proceed to their outgoing flows.
	return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
}
