package impl

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/rs/zerolog/log"
)

// NullNodeHandler is a Null Object implementation of the NodeHandler interface.
// It logs an error when executed, as it represents an unsupported or missing node type.
type NullNodeHandler struct{}

func (h *NullNodeHandler) Execute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	log.Error().
		Str("instance_id", instance.ID.String()).
		Str("node_id", node.ID).
		Str("node_type", string(node.Type)).
		Msg("NullNodeHandler: execution called for unsupported node type")
	return nil
}

func (h *NullNodeHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	return h.Execute(ctx, instance, def, node, iterationID)
}
