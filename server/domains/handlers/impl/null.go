package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/metis/server/domains/entities"
)

// NullNodeHandler answers for a step the engine has no handler for.
//
// It used to log the step and return nil, which parked the token there with
// nothing coming to move it: the instance read as running and never raised an
// incident. Deploy refuses such a step now, so this only meets a version
// stored before it did, and failing is the one answer that reaches someone —
// the caller of a start, or the incident list when a job gets here.
type NullNodeHandler struct{}

func (h *NullNodeHandler) Execute(_ context.Context, _ *entities.ProcessInstance, _ *entities.ProcessDefinition, node entities.Node, _ string) error {
	return fmt.Errorf("step %s has type %q, which the engine cannot run", node.ID, node.Type)
}

func (h *NullNodeHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	return h.Execute(ctx, instance, def, node, iterationID)
}
