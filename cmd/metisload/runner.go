package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// runner drives the virtual users: each one starts a request, finds its task,
// claims it, completes it and reads the instance list, then does it again,
// with no pause, until the run ends. All of them start at once, so the server
// meets the full concurrency in the first second rather than being ramped
// into it.
type runner struct {
	base       string
	token      string
	projectID  string
	users      int
	transport  *http.Transport
	timeout    time.Duration
	think      time.Duration
	record     *recorder
	sequence   atomic.Int64
	iterations atomic.Int64
}

func (r *runner) run(ctx context.Context) {
	var wg sync.WaitGroup
	for user := range r.users {
		wg.Go(func() { r.virtualUser(ctx, user) })
	}
	wg.Wait()
}

func (r *runner) virtualUser(ctx context.Context, user int) {
	c := &client{
		http:  &http.Client{Transport: r.transport, Timeout: r.timeout},
		base:  r.base,
		token: r.token,
		// One address per virtual user, in a documentation range that is
		// never anyone's real address (RFC 5737, TEST-NET-3).
		forwardedFor: fmt.Sprintf("203.0.%d.%d", 113+user/250, 1+user%250),
	}
	for ctx.Err() == nil {
		if r.iteration(ctx, c) {
			r.iterations.Add(1)
		}
	}
}

// pause waits between two calls of one virtual user: the think time, or the
// server's Retry-After when it shed the last call. A client that retries a 429
// or a 503 at once is a client that turns overload into an outage, and the
// SDK and the UI both wait; so does this.
func (r *runner) pause(ctx context.Context, err error) {
	wait := r.think
	var refused *apiError
	if errors.As(err, &refused) {
		wait = max(wait, refused.retryAfter)
	}
	if wait <= 0 {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(wait):
	}
}

// iteration runs one request from start to finish. It stops at the first
// failure: a task that could not be found cannot be claimed.
func (r *runner) iteration(ctx context.Context, c *client) bool {
	var instanceID, taskID string
	steps := []struct {
		operation string
		do        func() error
	}{
		{opStart, func() (err error) {
			instanceID, err = c.startInstance(ctx, r.projectID, int(r.sequence.Add(1)))
			return err
		}},
		{opFindTask, func() (err error) { taskID, err = c.findTask(ctx, r.projectID, instanceID); return err }},
		{opClaim, func() error { return c.claimTask(ctx, taskID) }},
		{opComplete, func() error { return c.completeTask(ctx, taskID) }},
		{opList, func() error { return c.listInstances(ctx, r.projectID) }},
	}
	for _, step := range steps {
		if !r.attempt(ctx, step.operation, step.do) {
			return false
		}
	}
	return true
}

// maxAttempts bounds how often one call is retried after being shed. A real
// client gives up eventually too; one that never does is a retry storm.
const maxAttempts = 5

// attempt makes one call, and makes it again after the server's Retry-After
// when the server shed it rather than refused it. A request abandoned half way
// because one call was shed would leave an instance open that a person would
// have finished, and the database would grow by work nobody did.
func (r *runner) attempt(ctx context.Context, operation string, do func() error) bool {
	for range maxAttempts {
		began := time.Now()
		err := do()
		if ctx.Err() != nil {
			// The run ended mid-call. That is the deadline, not the server.
			return false
		}
		r.record.observe(operation, time.Since(began), err)
		r.pause(ctx, err)
		if err == nil {
			return true
		}
		if !shed(err) {
			return false
		}
	}
	return false
}

// shed is a refusal that says "not now" rather than "no".
func shed(err error) bool {
	var refused *apiError
	return errors.As(err, &refused) && (refused.status == 429 || refused.status == 503)
}
