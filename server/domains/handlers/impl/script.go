package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/metis/internal/pkg/features"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

type ScriptTaskHandler struct {
	engine servicecontracts.EngineRunner
}

func (h *ScriptTaskHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	script := node.ScriptSource()
	if script == "" {
		return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
	}
	// Deploy refuses a script while the flag is off; this meets a version
	// stored before it did, or deployed while it was on.
	if !features.Enabled(features.ScriptTasks) {
		return logic.ErrScriptTasksOff(node.ID)
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
