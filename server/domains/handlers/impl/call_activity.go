package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/gobpm/server/domains/entities"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
)

type CallActivityHandler struct {
	engine servicecontracts.EngineRunner
}

func (h *CallActivityHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	calledElement := node.GetStringProperty("called_element")
	if calledElement == "" {
		return fmt.Errorf("call activity %s has no called_element property", node.ID)
	}

	version := 0
	if v, ok := node.Properties["called_element_version"].(float64); ok {
		version = int(v)
	} else if v, ok := node.Properties["called_element_version"].(int); ok {
		version = v
	}

	// Prepare variables based on In mapping
	vars := instance.Variables
	if mapping, ok := node.Properties["in_mapping"].(map[string]any); ok && len(mapping) > 0 {
		vars = make(map[string]any)
		for target, source := range mapping {
			if srcKey, ok := source.(string); ok {
				if val, ok := instance.Variables[srcKey]; ok {
					vars[target] = val
				}
			}
		}
	}

	// We start the sub-process and DO NOT call Proceed yet.
	// The sub-process completion will trigger resumption of this process.
	_, err := h.engine.StartSubProcess(ctx, instance.Project.ID, calledElement, version, vars, instance.ID, node.ID)
	return err
}
