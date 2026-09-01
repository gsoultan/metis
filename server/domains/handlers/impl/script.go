package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

type ScriptTaskHandler struct {
	engine servicecontracts.EngineRunner
}

func (h *ScriptTaskHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	script := node.Script
	if script == "" {
		script = node.Condition
	}
	if s, ok := node.Properties["script"].(string); ok && script == "" {
		script = s
	}

	if script == "" {
		return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
	}

	// The variables are applied only once the script has finished, so a script
	// that fails partway leaves the instance as it was rather than half-written.
	updated, err := logic.RunScript(ctx, script, instance.Variables)
	if err != nil {
		return fmt.Errorf("script task %s: %w", node.ID, err)
	}
	for k, v := range updated {
		instance.SetVariable(k, v)
	}

	return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
}
