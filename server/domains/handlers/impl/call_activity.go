package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

type CallActivityHandler struct {
	engine servicecontracts.EngineRunner
}

func (h *CallActivityHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	// BPMN calls it calledElement; the property panel asks for "the process to
	// call" and writes called_process_key. Both are accepted, because a process
	// imported from BPMN XML and one drawn in the designer are the same thing
	// and neither should be told it has no process to call. Without this a call
	// activity built in the designer failed on its first run with "has no
	// called_element property".
	calledElement := node.GetStringProperty("called_element")
	if calledElement == "" {
		calledElement = node.GetStringProperty("called_process_key")
	}
	if calledElement == "" {
		return fmt.Errorf("call activity %s does not say which process to call", node.ID)
	}

	version := intProperty(node, "called_element_version")
	if version == 0 {
		version = intProperty(node, "called_process_version")
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

// intProperty reads a whole number from the free-form property bag.
//
// It arrives as a float when it has been through JSON and as an int when it has
// not, and a version of zero means "the current one" either way.
func intProperty(node entities.Node, key string) int {
	switch v := node.Properties[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	default:
		return 0
	}
}
