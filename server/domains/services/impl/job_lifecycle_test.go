package impl

import (
	"context"
	"testing"
	"time"
)

func TestDrainWaitsForWorkAlreadyStarted(t *testing.T) {
	w := &workerLifecycle{}
	if !w.begin() {
		t.Fatal("a fresh worker refused to start a job")
	}

	finished := make(chan struct{})
	go func() {
		time.Sleep(30 * time.Millisecond)
		// The order matters, and had it the other way round this test was
		// racy against itself. done() is what releases wait(), so with
		// close(finished) after it the drain could return, be scheduled, and
		// read finished before this goroutine had closed it — a failure
		// reported as "the drain returned before the job finished" while the
		// drain had done exactly what it should. Reproduced on this machine
		// roughly twice in 2,000 runs under -race, and on CI often enough to
		// turn an unrelated Dependabot pull request red.
		//
		// Closing first makes the assertion sound: the channel now stands for
		// "the job's work is complete", and the only way to observe it closed
		// after wait() returns is for wait() to have waited.
		close(finished)
		w.done()
	}()

	w.stopping()
	if !w.wait(2 * time.Second) {
		t.Fatal("the drain gave up on a job that was about to finish")
	}
	select {
	case <-finished:
	default:
		t.Fatal("the drain returned before the job finished")
	}
}

/*
 * A pod must not refuse to terminate because one partner call is hanging.
 *
 * The budget is the whole point: waiting forever turns a stuck downstream into
 * a deploy that never completes. A job that overruns is left to its lease,
 * which is the behaviour that already existed.
 */
func TestDrainGivesUpAtItsBudget(t *testing.T) {
	w := &workerLifecycle{}
	if !w.begin() {
		t.Fatal("a fresh worker refused to start a job")
	}
	defer w.done()

	w.stopping()
	start := time.Now()
	if w.wait(40 * time.Millisecond) {
		t.Fatal("the drain reported success while a job was still running")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("the drain waited %s, far past its budget", elapsed)
	}
}

// Once stopping has begun, a tick that fires must not claim more work: it would
// be started only to be abandoned.
func TestNoNewWorkIsStartedOnceStopping(t *testing.T) {
	w := &workerLifecycle{}
	w.stopping()
	if w.begin() {
		t.Fatal("a stopping worker accepted a new job")
	}
}

func TestDrainOfAnIdleWorkerReturnsAtOnce(t *testing.T) {
	w := &workerLifecycle{}
	w.stopping()
	if !w.wait(0) {
		t.Fatal("an idle worker did not drain immediately")
	}
}

/*
 * The write that has to survive the cancellation.
 *
 * A job's final status write used to ride the same context shutdown had just
 * cancelled, so it failed and the row kept this worker's lock until the lease
 * expired. Detaching keeps the values — the worker's system marker among them,
 * without which the strict tenant scope refuses the update — and drops only the
 * cancellation.
 */
func TestDetachSurvivesCancellationButKeepsValues(t *testing.T) {
	type markerKey struct{}
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), markerKey{}, "system"))
	cancel()

	if parent.Err() == nil {
		t.Fatal("the parent should already be cancelled")
	}

	detached, release := detach(parent, time.Second)
	defer release()

	if detached.Err() != nil {
		t.Fatalf("the detached context is already done: %v", detached.Err())
	}
	if detached.Value(markerKey{}) != "system" {
		t.Fatal("the detached context lost the values the write needs")
	}
	if _, ok := detached.Deadline(); !ok {
		t.Fatal("the detached context has no deadline, so a shutdown could hang on it")
	}
}

func TestShutdownDrainIsConfigurable(t *testing.T) {
	t.Setenv(envShutdownDrain, "")
	if ShutdownDrain() != defaultShutdownDrain {
		t.Fatalf("default is %s", ShutdownDrain())
	}
	t.Setenv(envShutdownDrain, "45s")
	if ShutdownDrain() != 45*time.Second {
		t.Fatalf("override gave %s", ShutdownDrain())
	}
	// A typo must not produce an unbounded wait.
	t.Setenv(envShutdownDrain, "ages")
	if ShutdownDrain() != defaultShutdownDrain {
		t.Fatalf("nonsense gave %s", ShutdownDrain())
	}
}
