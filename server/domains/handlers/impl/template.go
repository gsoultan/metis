package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/metis/server/domains/entities"
	handlercontracts "github.com/gsoultan/metis/server/domains/handlers/contracts"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
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
	if instance.IsMultiInstanceActive(node.ID) {
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

	instance.StartMultiInstance(node.ID, total)
	instance.RemoveTokenByNode(&node)

	if node.MultiInstanceType == "parallel" {
		for i := range total {
			instance.AddTokenWithIteration(&node, fmt.Sprintf("%d", i))
		}

		if err := t.engine.UpdateInstance(ctx, *instance); err != nil {
			return err
		}

		// Bound immediately before each iteration runs. These execute one after
		// another on this goroutine, and a task takes its own copy of the
		// variables as it starts — a service task snapshots them into its job —
		// so each one leaves with its own item.
		for i := range total {
			entities.BindMultiInstanceElement(instance, node, collection, i)
			if err := t.engine.ExecuteNodeIteration(ctx, instance, def, node.ID, fmt.Sprintf("%d", i)); err != nil {
				return err
			}
		}
		return nil
	}

	if node.MultiInstanceType == "sequential" {
		instance.AddTokenWithIteration(&node, "0")
		entities.BindMultiInstanceElement(instance, node, collection, 0)
		if err := t.engine.UpdateInstance(ctx, *instance); err != nil {
			return err
		}
		// The rest follow one at a time, each started when its predecessor
		// finishes; see the engine's multi-instance completion check.
		return t.engine.ExecuteNodeIteration(ctx, instance, def, node.ID, "0")
	}

	return nil
}
