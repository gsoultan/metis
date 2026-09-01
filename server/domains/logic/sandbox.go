package logic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gsoultan/metis/internal/pkg/envvar"

	"github.com/dop251/goja"
	"github.com/rs/zerolog/log"
)

// ErrScriptTimeout is returned when user-supplied script exceeds its budget.
var ErrScriptTimeout = errors.New("script exceeded its execution budget")

// ErrScriptAbandoned is returned when a script ignored its interrupt and the
// caller stopped waiting. It is deliberately distinct from ErrScriptTimeout:
// a timeout means the sandbox stopped the script, an abandonment means it could
// not, and the script is still running on a detached goroutine. That is worth
// alerting on — see the note on interruptGrace.
var ErrScriptAbandoned = errors.New("script ignored its interrupt and was abandoned")

const (
	defaultScriptTimeout = 5 * time.Second
	envScriptTimeout     = "METIS_SCRIPT_TIMEOUT"

	// interruptGrace is how long to keep waiting after the interrupt fires,
	// before giving up on the script entirely.
	//
	// goja honours interrupts only between statements, so a script inside a
	// single long native call never sees one. Measured: the gateway condition
	// `new Array(1e9).join('x')` runs for 37s against a 200ms budget — 188 times
	// over. Every token through that gateway would hold a job worker for the
	// duration, and enough of them stop the engine, which is precisely the
	// denial of service the budget exists to prevent.
	//
	// Waiting a short grace period lets a well-behaved script unwind and report
	// ErrScriptTimeout properly; past that the caller is released regardless.
	interruptGrace = 500 * time.Millisecond

	// maxCallStackSize bounds runaway recursion before it exhausts memory.
	maxCallStackSize = 2048
)

// ScriptTimeout returns the wall-clock budget for a single script, gateway
// condition or DMN cell evaluation. Override with METIS_SCRIPT_TIMEOUT (a Go
// duration such as "2s").
func ScriptTimeout() time.Duration {
	raw := envvar.Get(envScriptTimeout)
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
// The script runs on its own goroutine so that a script which ignores its
// interrupt cannot hold the caller. Callers must therefore treat vm as consumed:
// both call sites build a fresh runtime per evaluation, so an abandoned script
// keeps sole ownership of its own and races nothing.
//
// This bounds worker starvation, not memory. An abandoned script goes on
// allocating until it finishes, and goja offers no heap limit to cap it with.
// The real fix is to stop accepting arbitrary JavaScript in conditions at all —
// execution-plan.md Phase 2.2 puts gateway conditions on FEEL and leaves
// JavaScript behind an explicit opt-in that is off by default.
func RunSandboxed(ctx context.Context, vm *goja.Runtime, budget time.Duration, fn func() (goja.Value, error)) (goja.Value, error) {
	if budget <= 0 {
		budget = ScriptTimeout()
	}

	timer := time.AfterFunc(budget, func() {
		vm.Interrupt(ErrScriptTimeout)
	})
	defer timer.Stop()

	// Buffered so an abandoned script can still send its result and exit rather
	// than blocking on a channel nobody reads again.
	outcome := make(chan scriptOutcome, 1)
	go func() {
		defer func() {
			// A panic inside user script must not take the process with it.
			if r := recover(); r != nil {
				outcome <- scriptOutcome{err: fmt.Errorf("script panicked: %v", r)}
			}
		}()
		value, err := fn()
		outcome <- scriptOutcome{value: value, err: err}
	}()

	// Cancelling the caller's context also stops the script, so a client
	// disconnect or shutdown does not leave a runaway evaluation behind.
	stopWatching := make(chan struct{})
	defer close(stopWatching)
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(ctx.Err())
		case <-stopWatching:
		}
	}()

	abandon := time.NewTimer(budget + interruptGrace)
	defer abandon.Stop()

	select {
	case result := <-outcome:
		// Only safe to clear once the script has actually stopped; clearing
		// while it still runs would hand a runaway script its interrupt back.
		vm.ClearInterrupt()
		if result.err != nil {
			var interrupted *goja.InterruptedError
			if errors.As(result.err, &interrupted) {
				return nil, fmt.Errorf("%w after %s", ErrScriptTimeout, budget)
			}
			return nil, result.err
		}
		return result.value, nil

	case <-abandon.C:
		// The script is inside a native call that cannot be pre-empted. Release
		// the caller and let it burn itself out.
		log.Warn().
			Dur("budget", budget).
			Msg("A script ignored its interrupt and was abandoned; it still holds a goroutine and its memory")
		return nil, fmt.Errorf("%w after %s", ErrScriptAbandoned, budget+interruptGrace)
	}
}

// scriptOutcome carries a script's result back from its own goroutine.
type scriptOutcome struct {
	value goja.Value
	err   error
}

// NewSandboxedRuntime returns a goja runtime with the ambient globals that
// leak host capability removed. goja ships no filesystem or network bindings,
// so the remaining risk is resource exhaustion rather than host access, but
// removing these keeps the surface small and the intent explicit.
func NewSandboxedRuntime() *goja.Runtime {
	vm := goja.New()
	// Recursion is one of the few exhaustion paths goja can bound directly, so
	// bound it. The interrupt already stops recursion between statements; this
	// keeps the stack from growing far in the interval before it fires.
	vm.SetMaxCallStackSize(maxCallStackSize)
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
