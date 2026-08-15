package logic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/dop251/goja"
	"github.com/rs/zerolog/log"
)

// ErrScriptTimeout is returned when user-supplied script exceeds its budget.
var ErrScriptTimeout = errors.New("script exceeded its execution budget")

const (
	defaultScriptTimeout = 5 * time.Second
	envScriptTimeout     = "GOBPM_SCRIPT_TIMEOUT"
)

// ScriptTimeout returns the wall-clock budget for a single script, gateway
// condition or DMN cell evaluation. Override with GOBPM_SCRIPT_TIMEOUT (a Go
// duration such as "2s").
func ScriptTimeout() time.Duration {
	raw := os.Getenv(envScriptTimeout)
	if raw == "" {
		return defaultScriptTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultScriptTimeout
	}
	return d
}

// RunSandboxed executes fn against vm under a wall-clock budget, interrupting
// the runtime if it overruns.
//
// Process definitions — script tasks, gateway conditions and DMN cells — are
// authored by users and executed on the server. Without an interrupt a single
// `while(true){}` blocks its goroutine and its database transaction forever;
// enough of them exhaust the job worker pool and the engine stops processing
// work entirely. goja only honours interrupts between statements, so this
// bounds loops but cannot pre-empt a single pathological native call.
//
// The runtime is left interrupted-free on return so vm can be reused.
func RunSandboxed(ctx context.Context, vm *goja.Runtime, budget time.Duration, fn func() (goja.Value, error)) (goja.Value, error) {
	if budget <= 0 {
		budget = ScriptTimeout()
	}

	timer := time.AfterFunc(budget, func() {
		vm.Interrupt(ErrScriptTimeout)
	})
	defer func() {
		timer.Stop()
		vm.ClearInterrupt()
	}()

	// Cancelling the caller's context also stops the script, so a client
	// disconnect or shutdown does not leave a runaway evaluation behind.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(ctx.Err())
		case <-done:
		}
	}()

	value, err := fn()
	if err != nil {
		var interrupted *goja.InterruptedError
		if errors.As(err, &interrupted) {
			return nil, fmt.Errorf("%w after %s", ErrScriptTimeout, budget)
		}
		return nil, err
	}
	return value, nil
}

// NewSandboxedRuntime returns a goja runtime with the ambient globals that
// leak host capability removed. goja ships no filesystem or network bindings,
// so the remaining risk is resource exhaustion rather than host access, but
// removing these keeps the surface small and the intent explicit.
func NewSandboxedRuntime() *goja.Runtime {
	vm := goja.New()
	for _, global := range []string{"eval", "Function"} {
		// Removing these is a security control, not a tidy-up: process
		// definitions are untrusted input, and a runtime that still has eval can
		// build code the sandbox was meant to keep out. A refusal here is loud
		// rather than silent.
		if err := vm.GlobalObject().Delete(global); err != nil {
			log.Error().Err(err).Str("global", global).
				Msg("Could not remove a global from the script sandbox; scripts may reach it")
		}
	}
	return vm
}

// maxScriptOutputBytes bounds a value returned from user script so a script
// cannot exhaust memory by building a huge string.
const maxScriptOutputBytes = 1 << 20

// TruncateScriptOutput bounds a string produced by user script.
func TruncateScriptOutput(s string) string {
	if len(s) <= maxScriptOutputBytes {
		return s
	}
	return s[:maxScriptOutputBytes] + "…(truncated)"
}
