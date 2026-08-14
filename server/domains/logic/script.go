package logic

import (
	"context"
	"fmt"
	"maps"

	"github.com/dop251/goja"
)

// RunScript executes a user-authored script against a copy of vars and returns
// the resulting variable set. vars is never mutated: a script that fails leaves
// the caller's variables untouched rather than half-applied.
//
// Two writing styles are supported and they agree with each other:
//
//	total = 99            // plain assignment to a bound name
//	setVar("total", 99)   // the bound helper, which also creates new variables
//
// setVar updates the script's own binding as well as the result set. That is
// load-bearing, not a convenience: the sync-back at the end reads every bound
// name out of the runtime, so a setVar that left the binding alone would have
// its write overwritten by the value the script started with — silently, and
// only for variables the process already had.
//
// This is the single implementation of script execution. It previously existed
// twice, once in the script task handler and once on the engine, and both copies
// carried that same bug.
func RunScript(ctx context.Context, script string, vars map[string]any) (map[string]any, error) {
	vm := NewSandboxedRuntime()

	updated := maps.Clone(vars)
	if updated == nil {
		updated = make(map[string]any, 1)
	}

	for k, v := range vars {
		if err := vm.Set(k, v); err != nil {
			return nil, fmt.Errorf("bind variable %q into script scope: %w", k, err)
		}
	}

	var bindErr error
	if err := vm.Set("setVar", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return goja.Undefined()
		}
		name := call.Arguments[0].String()
		val := call.Arguments[1].Export()
		updated[name] = val
		if err := vm.Set(name, val); err != nil && bindErr == nil {
			bindErr = fmt.Errorf("setVar %q: %w", name, err)
		}
		return goja.Undefined()
	}); err != nil {
		return nil, fmt.Errorf("bind setVar helper into script scope: %w", err)
	}

	// Script tasks, gateway conditions and DMN cells are authored by users and
	// executed on the server, so the run is bounded.
	if _, err := RunSandboxed(ctx, vm, ScriptTimeout(), func() (goja.Value, error) {
		return vm.RunString(script)
	}); err != nil {
		return nil, fmt.Errorf("script execution failed: %w", err)
	}
	if bindErr != nil {
		return nil, bindErr
	}

	// Pick up plain assignments to the variables the script was given.
	for k := range vars {
		if val := vm.Get(k); val != nil {
			updated[k] = val.Export()
		}
	}

	return updated, nil
}
