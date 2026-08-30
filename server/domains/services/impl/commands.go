package impl

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"

	"github.com/google/uuid"
)

type engineInternal interface {
	executeNodeInternal(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string, iterationID string) error
	proceedInternal(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string, iterationID string) error
	startProcessInternal(ctx context.Context, projectID uuid.UUID, definitionKey string, version int, vars map[string]any, parentInstanceID uuid.UUID, parentNodeID string) (uuid.UUID, error)
}

// ExecuteNodeCommand encapsulates the execution of a single BPMN node.
type ExecuteNodeCommand struct {
	engine      engineInternal
	instance    *entities.ProcessInstance
	def         *entities.ProcessDefinition
	nodeID      string
	iterationID string
}

func NewExecuteNodeCommand(engine engineInternal, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string, iterationID string) contracts.Command {
	return &ExecuteNodeCommand{engine: engine, instance: instance, def: def, nodeID: nodeID, iterationID: iterationID}
}

func (c *ExecuteNodeCommand) Execute(ctx context.Context) error {
	return c.engine.executeNodeInternal(ctx, c.instance, c.def, c.nodeID, c.iterationID)
}

// ProceedCommand encapsulates the movement from one node to its successors.
type ProceedCommand struct {
	engine      engineInternal
	instance    *entities.ProcessInstance
	def         *entities.ProcessDefinition
	nodeID      string
	iterationID string
}

func NewProceedCommand(engine engineInternal, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string, iterationID string) contracts.Command {
	return &ProceedCommand{engine: engine, instance: instance, def: def, nodeID: nodeID, iterationID: iterationID}
}

func (c *ProceedCommand) Execute(ctx context.Context) error {
	return c.engine.proceedInternal(ctx, c.instance, c.def, c.nodeID, c.iterationID)
}

// StartProcessCommand encapsulates the starting of a new process instance.
type StartProcessCommand struct {
	engine           engineInternal
	projectID        uuid.UUID
	definitionKey    string
	version          int
	vars             map[string]any
	parentInstanceID uuid.UUID
	parentNodeID     string
	InstanceID       uuid.UUID // Output field
}

func NewStartProcessCommand(engine engineInternal, projectID uuid.UUID, definitionKey string, version int, vars map[string]any, parentInstanceID uuid.UUID, parentNodeID string) *StartProcessCommand {
	return &StartProcessCommand{engine: engine, projectID: projectID, definitionKey: definitionKey, version: version, vars: vars, parentInstanceID: parentInstanceID, parentNodeID: parentNodeID}
}

func (c *StartProcessCommand) Execute(ctx context.Context) error {
	id, err := c.engine.startProcessInternal(ctx, c.projectID, c.definitionKey, c.version, c.vars, c.parentInstanceID, c.parentNodeID)
	c.InstanceID = id
	return err
}
