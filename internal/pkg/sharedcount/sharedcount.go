// Package sharedcount makes a per-process count into a per-installation one.
//
// Rate limits and circuit breakers were held in memory, so N replicas applied
// each limit N times over: a partner's quota exceeded N-fold, an inbound limit
// admitting N times what was configured, and N breakers each deciding on their
// own whether a downstream was healthy.
//
// The obvious fix — read and write a shared counter on every request — is not
// available. The inbound limiter runs on every HTTP request, and a database
// round trip there would cost more than the limit protects. So counting stays
// local and only the *totals* are exchanged, on a timer.
//
// What that buys is a bounded overshoot rather than an exact limit. Between two
// flushes a replica does not know what the others have counted, so the
// installation can admit up to one interval's worth of traffic per replica
// beyond the limit. With a 5s interval against a per-minute limit that is about
// a twelfth of the limit per replica — against N times the limit, forever,
// which is what it replaces. An exact distributed limit is available at the
// price of a round trip per request, and that price is not worth paying to
// stop a burst that lasts five seconds.
package sharedcount

import (
	"context"
	"sync"
	"time"

	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/rs/zerolog/log"
)

// Counter tracks one scope of counts across replicas.
type Counter struct {
	repo    contracts.SharedCounterRepository
	scope   string
	replica string
	window  time.Duration

	mu sync.Mutex
	// local is what this replica has counted in the current window, and is the
	// only thing the hot path touches.
	local map[string]int64
	// remote is what every *other* replica had counted as of the last exchange.
	remote map[string]int64
	// windowStart is the window local and remote describe.
	windowStart time.Time
}

// New creates a counter for one scope.
//
// The replica identifier has to be stable for the process and unique across
// them: it is part of the primary key, so two replicas sharing one would
// overwrite each other's contribution and the total would be wrong in the
// permissive direction.
func New(repo contracts.SharedCounterRepository, scope, replica string, window time.Duration) *Counter {
	return &Counter{
		repo:    repo,
		scope:   scope,
		replica: replica,
		window:  window,
		local:   make(map[string]int64),
		remote:  make(map[string]int64),
	}
}

// Add records one occurrence and returns the installation's best-known total,
// including it.
//
// In memory only. The returned total is this replica's own count plus what the
// others had reported at the last exchange, so it is a lower bound on the truth
// and never an over-count — a limiter built on it refuses late rather than
// early, which is the right direction for a false positive.
func (c *Counter) Add(key string, now time.Time) int64 {
	windowStart := now.Truncate(c.window)

	c.mu.Lock()
	defer c.mu.Unlock()

	if !windowStart.Equal(c.windowStart) {
		// A new window is a fresh count everywhere. Keeping the old totals
		// would carry a closed window's traffic into an open one.
		c.local = make(map[string]int64, len(c.local))
		c.remote = make(map[string]int64)
		c.windowStart = windowStart
	}

	c.local[key]++
	return c.local[key] + c.remote[key]
}

// Total reports the best-known total without recording anything.
func (c *Counter) Total(key string, now time.Time) int64 {
	windowStart := now.Truncate(c.window)

	c.mu.Lock()
	defer c.mu.Unlock()
	if !windowStart.Equal(c.windowStart) {
		return 0
	}
	return c.local[key] + c.remote[key]
}

// Keys returns the keys this replica has counted in the current window.
func (c *Counter) Keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	keys := make([]string, 0, len(c.local))
	for key := range c.local {
		keys = append(keys, key)
	}
	return keys
}

// Exchange publishes this replica's counts and reads back everyone's totals.
//
// Errors are logged and dropped rather than returned to a caller who could only
// log them too. A failed exchange leaves the replica enforcing its own count,
// which is the pre-existing behaviour — the limit degrades to per-process
// rather than disappearing.
func (c *Counter) Exchange(ctx context.Context) {
	c.mu.Lock()
	windowStart := c.windowStart
	snapshot := make(map[string]int64, len(c.local))
	for key, count := range c.local {
		snapshot[key] = count
	}
	c.mu.Unlock()

	if windowStart.IsZero() || len(snapshot) == 0 {
		return
	}

	for key, count := range snapshot {
		if err := c.repo.Record(ctx, c.scope, key, c.replica, windowStart, count); err != nil {
			log.Warn().Err(err).Str("scope", c.scope).
				Msg("Could not publish a shared count; this replica is enforcing its own share of the limit until the next exchange.")
			return
		}
	}

	keys := make([]string, 0, len(snapshot))
	for key := range snapshot {
		keys = append(keys, key)
	}

	totals, err := c.repo.Totals(ctx, c.scope, keys, windowStart)
	if err != nil {
		log.Warn().Err(err).Str("scope", c.scope).
			Msg("Could not read shared counts; this replica is enforcing its own share of the limit until the next exchange.")
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !windowStart.Equal(c.windowStart) {
		// The window rolled while the exchange was in flight. Applying these
		// totals would attribute a closed window's traffic to an open one.
		return
	}
	for key, total := range totals {
		// Totals includes this replica's own contribution, which local already
		// holds — subtracting it is what keeps Add from counting twice.
		if others := total - snapshot[key]; others > 0 {
			c.remote[key] = others
		} else {
			delete(c.remote, key)
		}
	}
}

// Run exchanges on a timer until the context is cancelled.
func (c *Counter) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.Exchange(ctx)
		}
	}
}
