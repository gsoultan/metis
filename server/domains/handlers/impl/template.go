package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/gobpm/server/domains/entities"
	handlercontracts "github.com/gsoultan/gobpm/server/domains/handlers/contracts"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/rs/zerolog/log"
)

// NodeHandlerTemplate provides a template for node execution.
type NodeHandlerTemplate struct {
	engine   servicecontracts.EngineRunner
	internal handlercontracts.InternalNodeHandler
}

func NewNodeHandlerTemplate(engine servicecontracts.EngineRunner, internal handlercontracts.InternalNodeHandler) *NodeHandlerTemplate {
	return &NodeHandlerTemplate{engine: engine, internal: internal}
}

func (t *NodeHandlerTemplate) Execute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	log.Debug().
		Str("instance_id", instance.ID.String()).
		Str("node_id", node.ID).
		Str("node_type", string(node.Type)).
		Str("iteration_id", iterationID).
		Msg("NodeHandlerTemplate: starting execution")

	// Handle Multi-Instance Activation
	if iterationID == "" && node.MultiInstanceType != "" && node.MultiInstanceType != "none" {
		return t.handleMultiInstance(ctx, instance, def, node)
	}

	err := t.internal.DoExecute(ctx, instance, def, node, iterationID)
	if err != nil {
		log.Error().
			Err(err).
			Str("instance_id", instance.ID.String()).
			Str("node_id", node.ID).
			Msg("NodeHandlerTemplate: execution failed")
		return err
	}

	log.Debug().
		Str("instance_id", instance.ID.String()).
		Str("node_id", node.ID).
		Msg("NodeHandlerTemplate: execution completed")

	return nil
}

func (t *NodeHandlerTemplate) handleMultiInstance(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node) error {
	activeKey := fmt.Sprintf("_mi_%s_active", node.ID)
	if _, ok := instance.Variables[activeKey]; ok {
		return nil
	}

	total := 0
	var collection []any
	if node.Collection != "" {
		if val, ok := instance.Variables[node.Collection].([]any); ok {
			collection = val
			total = len(collection)
		}
	} else if node.LoopCardinality > 0 {
		total = node.LoopCardinality
	}

	if total <= 0 {
		return t.internal.DoExecute(ctx, instance, def, node, "")
	}

	instance.SetVariable(activeKey, true)
	instance.SetVariable(fmt.Sprintf("_mi_%s_completed", node.ID), 0)
	instance.SetVariable(fmt.Sprintf("_mi_%s_total", node.ID), total)
	instance.RemoveTokenByNode(&node)

	if node.MultiInstanceType == "parallel" {
		for i := 0; i < total; i++ {
			iterationID := fmt.Sprintf("%d", i)
			instance.AddTokenWithIteration(&node, iterationID)

			// Setup local variable for this iteration if needed
			if node.ElementVariable != "" && i < len(collection) {
				instance.SetVariable(fmt.Sprintf("_mi_var_%s_%s", node.ID, iterationID), collection[i])
			}
		}

		if err := t.engine.UpdateInstance(ctx, *instance); err != nil {
			return err
		}

		for i := 0; i < total; i++ {
			iterationID := fmt.Sprintf("%d", i)
			if err := t.engine.ExecuteNodeIteration(ctx, instance, def, node.ID, iterationID); err != nil {
				return err
			}
		}
	} else if node.MultiInstanceType == "sequential" {
		iterationID := "0"
		instance.AddTokenWithIteration(&node, iterationID)
		if node.ElementVariable != "" && len(collection) > 0 {
			instance.SetVariable(fmt.Sprintf("_mi_var_%s_%s", node.ID, iterationID), collection[0])
		}
		if err := t.engine.UpdateInstance(ctx, *instance); err != nil {
			return err
		}
		return t.engine.ExecuteNodeIteration(ctx, instance, def, node.ID, iterationID)
	}

	return nil
}
