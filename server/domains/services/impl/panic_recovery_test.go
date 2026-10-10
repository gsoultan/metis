package impl

import (
	"context"
	"errors"
	"testing"
)

// TestAPanickingMessageFailsAloneAndThePartitionKeepsServing: the partition
// workers run a message's correlation — user-authored definitions and
// decisions — in a goroutine nothing recovered, so one panic ended the
// process, and the broker redelivered the same message to the next replica.
func TestAPanickingMessageFailsAloneAndThePartitionKeepsServing(t *testing.T) {
	executor := newInboundPartitionExecutor(1, 1)
	t.Cleanup(executor.Stop)

	err := executor.Execute(t.Context(), "k", func(context.Context) error {
		panic("a defect in the engine")
	})
	if !errors.Is(err, errPanicked) {
		t.Fatalf("err = %v, want it to wrap errPanicked", err)
	}

	if err := executor.Execute(t.Context(), "k", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("the partition stopped serving after a panic: %v", err)
	}
}

func TestRunRecoveredPassesAnErrorThrough(t *testing.T) {
	want := errors.New("ordinary failure")
	if got := runRecovered("x", func() error { return want }); !errors.Is(got, want) || errors.Is(got, errPanicked) {
		t.Fatalf("got %v, want the error itself", got)
	}
	if got := runRecovered("x", func() error { return nil }); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}
