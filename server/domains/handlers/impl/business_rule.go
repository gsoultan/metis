package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/domains/services/contracts"
)

type BusinessRuleTaskHandler struct {
	engine          contracts.EngineRunner
	decisionService contracts.DecisionService
}

func (h *BusinessRuleTaskHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	decisionKey := node.GetStringProperty("decision_key")
	if decisionKey == "" {
		// If no decision key, treat as pass-through
		return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
	}

	decisionVersion := 0
	if v, ok := node.Properties["decision_version"].(float64); ok {
		decisionVersion = int(v)
	} else if v, ok := node.Properties["decision_version"].(int); ok {
		decisionVersion = v
	}

	// Prepare inputs based on input mapping
	inputs := instance.Variables
	if mapping, ok := node.Properties["input_mapping"].(map[string]any); ok && len(mapping) > 0 {
		inputs = make(map[string]any)
		for target, source := range mapping {
			if srcKey, ok := source.(string); ok {
				if val, ok := instance.Variables[srcKey]; ok {
					inputs[target] = val
				}
			}
		}
	}

	result, err := h.decisionService.Evaluate(ctx, decisionKey, decisionVersion, inputs)
	if err != nil {
		return fmt.Errorf("decision evaluation failed for node %s: %w", node.ID, err)
	}

	// Apply decision results to process variables based on output mapping
	if mapping, ok := node.Properties["output_mapping"].(map[string]any); ok {
		for target, source := range mapping {
			if srcKey, ok := source.(string); ok {
				if val, ok := result.Values[srcKey]; ok {
					instance.SetVariable(target, val)
				}
			}
		}
	} else {
		// Default: apply all results to process variables
		for k, v := range result.Values {
			instance.SetVariable(k, v)
		}
	}

	return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
}
