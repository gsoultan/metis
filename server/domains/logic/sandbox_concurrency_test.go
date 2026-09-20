package logic

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dop251/goja"
)

// The sandbox bounds wall-clock time, recursion and host capability, and it
// cannot bound memory: goja exposes no heap limit, so an abandoned script goes
// on allocating until it finishes. The only quantity left to bound is how many
// scripts are allocating at once, which is what the slots do.
//
// These tests pin the two properties that make that bound real: the limit is
// enforced, and an abandoned script keeps holding its slot. The second is the
// one worth guarding — releasing the slot when the *caller* gives up would
// leave the runaway uncounted, which is precisely the case this exists for.

// withSlots replaces the process-wide semaphore for one test and restores it,
// so a test can choose a limit.
func withSlots(t *testing.T, n int) {
	t.Helper()
	scriptSlotsMu.Lock()
	prev := scriptSlots
	scriptSlots = make(chan struct{}, n)
	scriptSlotsMu.Unlock()
	t.Cleanup(func() {
		scriptSlotsMu.Lock()
		scriptSlots = prev
		scriptSlotsMu.Unlock()
	})
}

func TestRunSandboxed_BoundsConcurrentScripts(t *testing.T) {
	withSlots(t, 2)

	var mu sync.Mutex
	running, peak := 0, 0

	release := make(chan struct{})
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			vm := NewSandboxedRuntime()
			_, _ = RunSandboxed(context.Background(), vm, 5*time.Second, func() (goja.Value, error) {
				mu.Lock()
				running++
				if running > peak {
					peak = running
				}
				mu.Unlock()

				<-release

				mu.Lock()
				running--
				mu.Unlock()
				return goja.Undefined(), nil
			})
		}()
	}

	// Let whatever can start, start, then read the high-water mark.
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	got := peak
	mu.Unlock()
	close(release)
	wg.Wait()

	if got > 2 {
		t.Errorf("as many as %d scripts ran at once against a limit of 2", got)
	}
	if got == 0 {
		t.Fatal("no script ran at all; the test proved nothing")
	}
}

// The property that makes the bound meaningful. A script that ignores its
// interrupt is abandoned by its caller and goes on allocating — so it must go
// on holding its slot too.
func TestRunSandboxed_AnAbandonedScriptKeepsItsSlot(t *testing.T) {
	withSlots(t, 1)

	stuck := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		vm := NewSandboxedRuntime()
		_, _ = RunSandboxed(context.Background(), vm, 100*time.Millisecond, func() (goja.Value, error) {
			<-stuck // never interruptible, like a long native call
			return goja.Undefined(), nil
		})
	}()

	// The caller gives up after its budget plus the grace period.
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the caller was never released from an uninterruptible script")
	}

	// The slot must still be held, because the script is still running.
	vm := NewSandboxedRuntime()
	_, err := RunSandboxed(context.Background(), vm, 200*time.Millisecond, func() (goja.Value, error) {
		return goja.Undefined(), nil
	})
	if !errors.Is(err, ErrScriptCapacity) {
		t.Errorf("got %v, want ErrScriptCapacity: the abandoned script released its slot "+
			"when its caller gave up, so a runaway allocator is no longer counted", err)
	}

	close(stuck)
}

func TestRunSandboxed_RefusesLegiblyWhenFull(t *testing.T) {
	withSlots(t, 1)

	hold := make(chan struct{})
	started := make(chan struct{})
	go func() {
		vm := NewSandboxedRuntime()
		_, _ = RunSandboxed(context.Background(), vm, 5*time.Second, func() (goja.Value, error) {
			close(started)
			<-hold
			return goja.Undefined(), nil
		})
	}()
	<-started

	vm := NewSandboxedRuntime()
	_, err := RunSandboxed(context.Background(), vm, 100*time.Millisecond, func() (goja.Value, error) {
		return goja.Undefined(), nil
	})
	if !errors.Is(err, ErrScriptCapacity) {
		t.Fatalf("got %v, want ErrScriptCapacity", err)
	}
	// The operator has to be able to act on it, which means knowing the knob.
	if !strings.Contains(err.Error(), envScriptConcurrency) {
		t.Errorf("the refusal does not name %s: %v", envScriptConcurrency, err)
	}

	close(hold)
}

// A slot freed inside the budget is waited for rather than refused: queueing is
// the correct behaviour under ordinary load, and only exhaustion is an error.
func TestRunSandboxed_WaitsForASlotRatherThanFailingImmediately(t *testing.T) {
	withSlots(t, 1)

	hold := make(chan struct{})
	started := make(chan struct{})
	go func() {
		vm := NewSandboxedRuntime()
		_, _ = RunSandboxed(context.Background(), vm, 5*time.Second, func() (goja.Value, error) {
			close(started)
			<-hold
			return goja.Undefined(), nil
		})
	}()
	<-started

	go func() {
		time.Sleep(150 * time.Millisecond)
		close(hold)
	}()

	vm := NewSandboxedRuntime()
	_, err := RunSandboxed(context.Background(), vm, 3*time.Second, func() (goja.Value, error) {
		return goja.Undefined(), nil
	})
	if err != nil {
		t.Errorf("a script that could have waited was refused: %v", err)
	}
}

func TestScriptConcurrency_Configuration(t *testing.T) {
	t.Run("defaults below the job worker count", func(t *testing.T) {
		t.Setenv(envScriptConcurrency, "")
		if got := ScriptConcurrency(); got != defaultScriptConcurrency {
			t.Errorf("ScriptConcurrency() = %d, want %d", got, defaultScriptConcurrency)
		}
	})

	t.Run("honours an override", func(t *testing.T) {
		t.Setenv(envScriptConcurrency, "9")
		if got := ScriptConcurrency(); got != 9 {
			t.Errorf("ScriptConcurrency() = %d, want 9", got)
		}
	})

	// Zero would stop the engine evaluating any decision at all. Nobody means
	// that, so it is a misconfiguration to warn about rather than to obey.
	for _, raw := range []string{"0", "-1", "lots", "3.5"} {
		t.Run("refuses "+raw, func(t *testing.T) {
			t.Setenv(envScriptConcurrency, raw)
			if got := ScriptConcurrency(); got != defaultScriptConcurrency {
				t.Errorf("ScriptConcurrency() = %d for %q, want the default %d",
					got, raw, defaultScriptConcurrency)
			}
		})
	}
}

// The count that explains an exhausted pool must be exact in both directions.
// A leaked +1 would make a healthy pool look permanently wedged; a missed one
// would hide the only cause an operator can act on.
func TestAbandonedScriptsAreCountedAndUncounted(t *testing.T) {
	withSlots(t, 2)
	before := abandonedScripts.Load()

	stuck := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		vm := NewSandboxedRuntime()
		_, _ = RunSandboxed(context.Background(), vm, 100*time.Millisecond, func() (goja.Value, error) {
			<-stuck
			return goja.Undefined(), nil
		})
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("caller was never released")
	}

	if got := abandonedScripts.Load(); got != before+1 {
		t.Errorf("abandoned count = %d, want %d while a runaway is still running", got, before+1)
	}

	// When it finally burns out the count must come back down, or the warning
	// it feeds becomes permanently wrong.
	close(stuck)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if abandonedScripts.Load() == before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("abandoned count stayed at %d after the script finished, want %d",
		abandonedScripts.Load(), before)
}

// A script that finishes in the moment between the timer firing and the
// abandon branch running must not be counted: that would leak a permanent +1.
func TestAScriptThatFinishesIsNotCountedAsAbandoned(t *testing.T) {
	withSlots(t, 2)
	before := abandonedScripts.Load()

	for range 25 {
		vm := NewSandboxedRuntime()
		_, _ = RunSandboxed(context.Background(), vm, 30*time.Millisecond, func() (goja.Value, error) {
			time.Sleep(25 * time.Millisecond) // lands either side of the budget
			return goja.Undefined(), nil
		})
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if abandonedScripts.Load() == before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("abandoned count settled at %d, want %d: a finished script was counted as abandoned",
		abandonedScripts.Load(), before)
}
