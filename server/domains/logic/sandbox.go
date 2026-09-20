package logic

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

// ErrScriptCapacity is returned when every script slot is occupied and one did
// not come free inside the caller's budget. It means the engine refused to
// start another allocating script, which is the point: see scriptSlots.
var ErrScriptCapacity = errors.New("no script slot came free within the budget")

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

	// defaultScriptConcurrency is how many scripts may execute at once.
	//
	// Deliberately below METIS_JOB_WORKERS (default 10). The sandbox cannot
	// bound a script's memory — goja exposes no heap limit — so the only
	// quantity left to bound is how many scripts are allocating simultaneously.
	// Peak script memory is that number times whatever one script can reach,
	// and without a cap the multiplier is the worker count.
	//
	// It is not 1: DMN cells and gateway conditions come through here too, and
	// serialising every decision in the installation behind one slot would make
	// a correctness control into a throughput bug.
	defaultScriptConcurrency = 4
	envScriptConcurrency     = "METIS_SCRIPT_CONCURRENCY"
)

// scriptSlots bounds concurrent script execution.
//
// A slot is taken before the script starts and released when its goroutine
// actually finishes — not when RunSandboxed returns. That difference is the
// whole control: a script that ignores its interrupt is abandoned by its
// caller and goes on allocating, and it keeps holding its slot while it does.
// So the number of scripts allocating at once is bounded even though the
// lifetime of any one of them is not.
//
// The consequence is deliberate and worth stating: enough simultaneously
// abandoned scripts will exhaust the slots, and further scripts then fail with
// ErrScriptCapacity instead of starting. A legible refusal under pressure is
// the better failure — the alternative is an unbounded number of runaway
// allocators and an out-of-memory kill that takes the whole process, including
// the instances that were behaving.
//
// Sized once. A limit that changed under a running engine would let the number
// of live scripts exceed whichever value was smaller.
var (
	scriptSlotsMu sync.Mutex
	scriptSlots   chan struct{}

	// abandonedScripts counts scripts whose caller gave up but which are still
	// running, and so are still holding a slot.
	//
	// This is the number that explains an exhausted pool. Without it an
	// operator sees every gateway condition and DMN cell in the installation
	// failing with no cause: the slots are the mechanism, but runaway scripts
	// are the reason, and only this distinguishes "busy" from "wedged".
	abandonedScripts atomic.Int64
)

// script lifecycle states, for deciding who owns the abandoned count.
const (
	scriptRunning int32 = iota
	scriptAbandoned
	scriptFinished
)

func slots() chan struct{} {
	scriptSlotsMu.Lock()
	defer scriptSlotsMu.Unlock()
	if scriptSlots == nil {
		scriptSlots = make(chan struct{}, ScriptConcurrency())
	}
	return scriptSlots
}

// ScriptConcurrency returns how many scripts may run at once. Override with
// METIS_SCRIPT_CONCURRENCY. Values below 1 are ignored: zero would stop the
// engine evaluating any decision at all, which is not a configuration anybody
// means to choose.
func ScriptConcurrency() int {
	raw := envvar.Get(envScriptConcurrency)
	if raw == "" {
		return defaultScriptConcurrency
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		log.Warn().
			Str("value", raw).
			Str("env", envScriptConcurrency).
			Int("using", defaultScriptConcurrency).
			Msg("Ignoring an unreadable script concurrency limit")
		return defaultScriptConcurrency
	}
	return n
}

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

	// Take a slot before starting. Waiting here rather than after means a
	// script under contention never begins allocating, which is the difference
	// between queueing and piling up.
	slot := slots()
	acquire, cancelAcquire := context.WithTimeout(ctx, budget)
	defer cancelAcquire()
	select {
	case slot <- struct{}{}:
	case <-acquire.Done():
		stuck := abandonedScripts.Load()
		// Warn rather than return quietly. An exhausted pool means decisions
		// are failing installation-wide, and the two causes need different
		// answers: genuine load wants a higher limit, scripts that ignored
		// their interrupt want fixing. Saying how many slots are held by the
		// latter is the difference between those.
		log.Warn().
			Int("limit", cap(slot)).
			Int64("held_by_abandoned_scripts", stuck).
			Str("env", envScriptConcurrency).
			Msg("Refused a script: every slot is in use. If the abandoned count is at the limit, " +
				"the pool is wedged by runaway scripts rather than busy.")
		return nil, fmt.Errorf("%w (limit %d, %d held by abandoned scripts, %s)",
			ErrScriptCapacity, cap(slot), stuck, envScriptConcurrency)
	}

	timer := time.AfterFunc(budget, func() {
		vm.Interrupt(ErrScriptTimeout)
	})
	defer timer.Stop()

	// Buffered so an abandoned script can still send its result and exit rather
	// than blocking on a channel nobody reads again.
	outcome := make(chan scriptOutcome, 1)
	// Owned jointly by this goroutine and the abandon branch below, so the
	// abandoned count is incremented at most once and always decremented.
	var lifecycle atomic.Int32
	go func() {
		// Released here, in the script's own goroutine, so an abandoned script
		// holds its slot for as long as it keeps allocating. Releasing in
		// RunSandboxed would hand the slot back the moment the caller gave up
		// and leave the runaway uncounted — which is the one case this exists
		// to bound.
		defer func() {
			<-slot
			if lifecycle.Swap(scriptFinished) == scriptAbandoned {
				abandonedScripts.Add(-1)
			}
		}()
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
		//
		// CompareAndSwap rather than a plain store: the script may have
		// finished in the moment between the timer firing and this line, and
		// counting it then would leak a permanent +1 that makes a healthy pool
		// look wedged forever.
		stuck := abandonedScripts.Load()
		if lifecycle.CompareAndSwap(scriptRunning, scriptAbandoned) {
			stuck = abandonedScripts.Add(1)
		}
		log.Warn().
			Dur("budget", budget).
			Int64("abandoned_and_still_running", stuck).
			Int("limit", cap(slot)).
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
