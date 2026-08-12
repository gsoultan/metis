package impl

import (
	"context"
	"fmt"

	"github.com/dop251/goja"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/domains/logic"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
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

	vm := logic.NewSandboxedRuntime()

	// Expose variables to JS
	for k, v := range instance.Variables {
		if err := vm.Set(k, v); err != nil {
			return fmt.Errorf("bind variable %q into script scope: %w", k, err)
		}
	}

	// Helper to set variables back
	if err := vm.Set("setVar", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) >= 2 {
			name := call.Arguments[0].String()
			val := call.Arguments[1].Export()
			instance.SetVariable(name, val)
		}
		return goja.Undefined()
	}); err != nil {
		return fmt.Errorf("bind setVar helper into script scope: %w", err)
	}

	// Script tasks are user-authored. RunSandboxed bounds them so a runaway
	// script cannot hold this goroutine and its transaction indefinitely.
	if _, err := logic.RunSandboxed(ctx, vm, logic.ScriptTimeout(), func() (goja.Value, error) {
		return vm.RunString(script)
	}); err != nil {
		return fmt.Errorf("script execution failed: %w", err)
	}

	// Also sync all variables that were modified in the root scope
	for k := range instance.Variables {
		val := vm.Get(k)
		if val != nil {
			instance.SetVariable(k, val.Export())
		}
	}

	return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
}
