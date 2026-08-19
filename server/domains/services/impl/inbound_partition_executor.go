package impl

import (
	"context"
	"errors"
	"hash/maphash"
	"sync"
)

var (
	errInboundPartitionExecutorStopped = errors.New("inbound partition executor stopped")
	errInboundPartitionTaskRequired    = errors.New("inbound partition task is required")
)

type inboundPartitionTask struct {
	ctx     context.Context
	execute func(context.Context) error
	result  chan error
}

type inboundPartitionExecutor struct {
	partitions []chan inboundPartitionTask
	seed       maphash.Seed
	stopCtx    context.Context
	stop       context.CancelCauseFunc
	stopOnce   sync.Once
	wg         sync.WaitGroup
}

func newInboundPartitionExecutor(partitionCount int, queueSize int) *inboundPartitionExecutor {
	partitionCount = max(partitionCount, 1)
	queueSize = max(queueSize, 1)

	stopCtx, stop := context.WithCancelCause(context.Background())
	executor := &inboundPartitionExecutor{
		partitions: make([]chan inboundPartitionTask, partitionCount),
		seed:       maphash.MakeSeed(),
		stopCtx:    stopCtx,
		stop:       stop,
	}

	for partitionIndex := range partitionCount {
		partitionQueue := make(chan inboundPartitionTask, queueSize)
		executor.partitions[partitionIndex] = partitionQueue
		executor.wg.Go(func() {
			executor.runPartitionWorker(partitionQueue)
		})
	}

	return executor
}

//nolint:contextcheck // the nil guard has no context to inherit; every other path threads ctx through
func (e *inboundPartitionExecutor) Execute(ctx context.Context, key string, execute func(context.Context) error) error {
	if execute == nil {
		return errInboundPartitionTaskRequired
	}

	if ctx == nil {
		ctx = context.Background()
	}

	partitionQueue := e.partitions[e.partitionIndex(key)]
	task := inboundPartitionTask{
		ctx:     ctx,
		execute: execute,
		result:  make(chan error, 1),
	}

	select {
	case <-e.stopCtx.Done():
		return errors.Join(errInboundPartitionExecutorStopped, context.Cause(e.stopCtx))
	case <-ctx.Done():
		return ctx.Err()
	case partitionQueue <- task:
	}

	select {
	case <-e.stopCtx.Done():
		return errors.Join(errInboundPartitionExecutorStopped, context.Cause(e.stopCtx))
	case <-ctx.Done():
		return ctx.Err()
	case err := <-task.result:
		return err
	}
}

func (e *inboundPartitionExecutor) runPartitionWorker(partitionQueue <-chan inboundPartitionTask) {
	for {
		select {
		case <-e.stopCtx.Done():
			return
		case task := <-partitionQueue:
			runCtx := task.ctx
			if runCtx == nil {
				runCtx = e.stopCtx
			}

			task.result <- task.execute(runCtx)
		}
	}
}

func (e *inboundPartitionExecutor) partitionIndex(key string) int {
	if len(e.partitions) == 1 {
		return 0
	}

	var hasher maphash.Hash
	hasher.SetSeed(e.seed)
	_, _ = hasher.WriteString(key)

	return int(hasher.Sum64() % uint64(len(e.partitions)))
}

func (e *inboundPartitionExecutor) Stop() {
	e.stopOnce.Do(func() {
		e.stop(errInboundPartitionExecutorStopped)
		e.wg.Wait()
	})
}
