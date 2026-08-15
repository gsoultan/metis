package logic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dop251/goja"
)

// TestRunSandboxed_InterruptsInfiniteLoop is the regression guard for the
// defect class where user-authored script ran with no interrupt: a script task,
// gateway condition or DMN cell containing `while(true){}` blocked its
// goroutine and its database transaction forever. Enough of them exhausted the
// job worker pool and the engine stopped processing work entirely.
func TestRunSandboxed_InterruptsInfiniteLoop(t *testing.T) {
	vm := NewSandboxedRuntime()

	start := time.Now()
	_, err := RunSandboxed(t.Context(), vm, 200*time.Millisecond, func() (goja.Value, error) {
		return vm.RunString("while (true) {}")
	})
	elapsed := time.Since(start)

	if !errors.Is(err, ErrScriptTimeout) {
		t.Fatalf("infinite loop was not interrupted: got %v, want ErrScriptTimeout", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("interrupt took %s, far beyond the 200ms budget", elapsed)
	}
}

func TestRunSandboxed_AllowsNormalScript(t *testing.T) {
	vm := NewSandboxedRuntime()

	value, err := RunSandboxed(t.Context(), vm, time.Second, func() (goja.Value, error) {
		return vm.RunString("1 + 2")
	})
	if err != nil {
		t.Fatalf("well-behaved script failed: %v", err)
	}
	if got := value.ToInteger(); got != 3 {
		t.Fatalf("got %d, want 3", got)
	}
}

// A cancelled request must not leave a runaway evaluation burning a goroutine.
func TestRunSandboxed_HonoursContextCancellation(t *testing.T) {
	vm := NewSandboxedRuntime()

	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(100*time.Millisecond, cancel)

	_, err := RunSandboxed(ctx, vm, time.Minute, func() (goja.Value, error) {
		return vm.RunString("while (true) {}")
	})
	if err == nil {
		t.Fatal("cancelled context did not stop the script")
	}
}

// The runtime must be reusable afterwards: a leftover interrupt would make the
// next evaluation fail for no reason.
func TestRunSandboxed_ClearsInterruptForReuse(t *testing.T) {
	vm := NewSandboxedRuntime()

	_, _ = RunSandboxed(t.Context(), vm, 100*time.Millisecond, func() (goja.Value, error) {
		return vm.RunString("while (true) {}")
	})

	value, err := RunSandboxed(t.Context(), vm, time.Second, func() (goja.Value, error) {
		return vm.RunString("40 + 2")
	})
	if err != nil {
		t.Fatalf("runtime was not reusable after an interrupt: %v", err)
	}
	if got := value.ToInteger(); got != 42 {
		t.Fatalf("got %d, want 42", got)
	}
}
