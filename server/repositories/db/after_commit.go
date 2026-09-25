package db

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"
)

// commitHooks are what should happen once a transaction has committed.
//
// They exist for the work that must not happen inside one: a network call
// made while a transaction is open holds its connection and its row locks for
// as long as somebody else's server takes to answer, and it happens even if
// the transaction then rolls back.
type commitHooks struct {
	mu  sync.Mutex
	fns []func()
}

type commitHooksKey struct{}

func withCommitHooks(ctx context.Context, hooks *commitHooks) context.Context {
	return context.WithValue(ctx, commitHooksKey{}, hooks)
}

func commitHooksFrom(ctx context.Context) (*commitHooks, bool) {
	hooks, ok := ctx.Value(commitHooksKey{}).(*commitHooks)
	return hooks, ok
}

func (h *commitHooks) add(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fns = append(h.fns, fn)
}

// mark and rewind let a savepoint that rolls back take its hooks with it: work
// undone to a savepoint did not happen, and neither should what follows it.
func (h *commitHooks) mark() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.fns)
}

func (h *commitHooks) rewind(to int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	clear(h.fns[to:])
	h.fns = h.fns[:to]
}

// run calls every hook in the order they were added. A hook that panics is
// logged and the rest still run: the transaction has committed, and one
// follow-up failing is no reason to skip the others.
func (h *commitHooks) run() {
	h.mu.Lock()
	fns := h.fns
	h.fns = nil
	h.mu.Unlock()
	for _, fn := range fns {
		runHook(fn)
	}
}

func runHook(fn func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Error().Interface("panic", recovered).Msg("Work scheduled for after a commit panicked")
		}
	}()
	fn()
}

// AfterCommit runs fn once the transaction ctx belongs to has committed, and
// never if it rolls back. Outside a transaction the work fn follows has
// already committed, so it runs now.
func AfterCommit(ctx context.Context, fn func()) {
	if hooks, ok := commitHooksFrom(ctx); ok {
		hooks.add(fn)
		return
	}
	fn()
}
