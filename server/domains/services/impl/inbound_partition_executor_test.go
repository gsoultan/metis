package impl

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestInboundPartitionExecutorExecuteValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setup       func(*inboundPartitionExecutor)
		execute     func(context.Context) error
		expectedErr error
	}{
		{
			name:        "nil task returns validation error",
			execute:     nil,
			expectedErr: errInboundPartitionTaskRequired,
		},
		{
			name: "stopped executor rejects new task",
			setup: func(executor *inboundPartitionExecutor) {
				executor.Stop()
			},
			execute: func(context.Context) error {
				return nil
			},
			expectedErr: errInboundPartitionExecutorStopped,
		},
	}

	for _, tc := range tests {

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			executor := newInboundPartitionExecutor(2, 2)
			t.Cleanup(executor.Stop)

			if tc.setup != nil {
				tc.setup(executor)
			}

			err := executor.Execute(t.Context(), "validation-key", tc.execute)
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("expected error to match %v, got %v", tc.expectedErr, err)
			}
		})
	}
}

func TestInboundPartitionExecutorSameKeySerializesExecution(t *testing.T) {
	t.Parallel()

	executor := newInboundPartitionExecutor(4, 4)
	t.Cleanup(executor.Stop)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	startedFirst := make(chan struct{})
	releaseFirst := make(chan struct{})
	var executionOrderMu sync.Mutex
	executionOrder := make([]int, 0, 2)

	errCh := make(chan error, 2)
	go func() {
		errCh <- executor.Execute(ctx, "same-key", func(context.Context) error {
			close(startedFirst)
			<-releaseFirst

			executionOrderMu.Lock()
			executionOrder = append(executionOrder, 1)
			executionOrderMu.Unlock()
			return nil
		})
	}()

	select {
	case <-startedFirst:
	case <-ctx.Done():
		t.Fatalf("first task did not start before timeout: %v", ctx.Err())
	}

	go func() {
		errCh <- executor.Execute(ctx, "same-key", func(context.Context) error {
			executionOrderMu.Lock()
			executionOrder = append(executionOrder, 2)
			executionOrderMu.Unlock()
			return nil
		})
	}()

	close(releaseFirst)

	for range 2 {
		if err := <-errCh; err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	}

	executionOrderMu.Lock()
	defer executionOrderMu.Unlock()
	if len(executionOrder) != 2 {
		t.Fatalf("expected two executions, got %d", len(executionOrder))
	}

	if executionOrder[0] != 1 || executionOrder[1] != 2 {
		t.Fatalf("expected serial execution order [1 2], got %v", executionOrder)
	}
}

func TestInboundPartitionExecutorDifferentKeysCanRunInParallel(t *testing.T) {
	t.Parallel()

	executor := newInboundPartitionExecutor(4, 2)
	t.Cleanup(executor.Stop)

	primaryKey := "partition-primary"
	primaryIndex := executor.partitionIndex(primaryKey)
	secondaryKey := ""
	for i := range 256 {
		candidateKey := fmt.Sprintf("partition-secondary-%d", i)
		if executor.partitionIndex(candidateKey) != primaryIndex {
			secondaryKey = candidateKey
			break
		}
	}
	if secondaryKey == "" {
		t.Fatal("failed to find keys mapped to different partitions")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	startedFirst := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondDone := make(chan struct{})
	errCh := make(chan error, 2)

	go func() {
		errCh <- executor.Execute(ctx, primaryKey, func(context.Context) error {
			close(startedFirst)
			<-releaseFirst
			return nil
		})
	}()

	select {
	case <-startedFirst:
	case <-ctx.Done():
		t.Fatalf("first task did not start before timeout: %v", ctx.Err())
	}

	go func() {
		errCh <- executor.Execute(ctx, secondaryKey, func(context.Context) error {
			close(secondDone)
			return nil
		})
	}()

	select {
	case <-secondDone:
	case <-ctx.Done():
		t.Fatalf("second task did not complete in parallel: %v", ctx.Err())
	}

	close(releaseFirst)

	for range 2 {
		if err := <-errCh; err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	}
}

func TestInboundPartitionExecutorQueuedTaskHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	executor := newInboundPartitionExecutor(1, 1)
	t.Cleanup(executor.Stop)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	startedFirst := make(chan struct{})
	releaseFirst := make(chan struct{})
	var releaseFirstOnce sync.Once
	releaseFirstTask := func() {
		releaseFirstOnce.Do(func() {
			close(releaseFirst)
		})
	}
	t.Cleanup(releaseFirstTask)

	errCh := make(chan error, 1)

	go func() {
		errCh <- executor.Execute(ctx, "shared-key", func(context.Context) error {
			close(startedFirst)
			select {
			case <-releaseFirst:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()

	select {
	case <-startedFirst:
	case <-ctx.Done():
		t.Fatalf("first task did not start before timeout: %v", ctx.Err())
	}

	queuedCtx, queuedCancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer queuedCancel()

	err := executor.Execute(queuedCtx, "shared-key", func(context.Context) error {
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected queued task cancellation error, got %v", err)
	}

	releaseFirstTask()
	if err = <-errCh; err != nil {
		t.Fatalf("expected nil error for first task, got %v", err)
	}
}
