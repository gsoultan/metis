package contracts

import (
	"context"

	"github.com/gsoultan/gobpm/server/domains/entities"
)

// NodeHandler defines the interface for handling BPMN node execution.
type NodeHandler interface {
	Execute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error
}

// InternalNodeHandler is an interface for specific node execution logic.
type InternalNodeHandler interface {
	DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error
}
