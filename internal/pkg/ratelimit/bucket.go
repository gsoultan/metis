// Package ratelimit keeps one process from spending a partner's whole quota.
//
// A process that starts a thousand instances at once will call the same endpoint
// a thousand times as fast as the job pool allows. Most APIs answer that with
// 429s for everyone using that account — including the other processes, and
// including whatever else in the business depends on the same integration. The
// engine is then the reason an unrelated team's work stopped.
//
// The limiter is a token bucket per target: a steady refill rate with a burst
// allowance, which is the shape API quotas are actually written in ("120
// requests a minute"). It answers with how long to wait rather than with a
// refusal, because being over a quota is a normal condition and not a failure —
// see the caller for why that distinction matters.
package ratelimit

import (
	"sync"

	"time"

	"github.com/gsoultan/metis/internal/pkg/sharedcount"
)

// Settings bound the group itself, not any one limit.
type Settings struct {
	// IdleTTL is how long a bucket nothing has drawn from is kept.
	//
	// Buckets are keyed by what is being called, which for a plain HTTP service
	// task is a host from a deployed process definition — untrusted input of
	// unbounded variety. Without eviction the map is a slow memory leak a
	// hostile definition can drive.
	IdleTTL time.Duration

	// MaxBuckets bounds the map for the same reason. Past the bound a new key
	// gets no bucket and its calls are unlimited: this protects a partner's
	// quota, not this engine's integrity, and failing open on the bookkeeping
	// leaves things exactly as they were before limits existed.
	MaxBuckets int
}

// DefaultSettings are sized for HTTP integrations.
func DefaultSettings() Settings {
	return Settings{IdleTTL: 15 * time.Minute, MaxBuckets: 1024}
}

// Group holds one bucket per target.
type Group struct {
	settings Settings
	now      func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket

	// shared carries this replica's draws to the others, so a partner's quota
	// is the installation's rather than each process's. Nil on a single-replica
	// deployment: the local bucket is then the whole truth.
	//
	// It does not replace the bucket. The bucket still shapes the rate — it is
	// what smooths a burst — and the shared count only refuses once the
	// installation as a whole has spent the minute's allowance.
	shared *sharedcount.Counter
}

// ShareVia gives the group a cross-replica counter.
func (g *Group) ShareVia(counter *sharedcount.Counter) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.shared = counter
}

type bucket struct {
	// perSecond is the refill rate and burst the size of the bucket.
	perSecond float64
	burst     float64

	tokens   float64
	lastFill time.Time
	lastUsed time.Time
}

// NewGroup returns a group of buckets.
func NewGroup(settings Settings) *Group {
	if settings.IdleTTL <= 0 {
		settings.IdleTTL = DefaultSettings().IdleTTL
	}
	if settings.MaxBuckets <= 0 {
		settings.MaxBuckets = DefaultSettings().MaxBuckets
	}
	return &Group{settings: settings, now: time.Now, buckets: make(map[string]*bucket)}
}

// Take asks for permission to make one call to key.
//
// perMinute is the target's quota, read from its configuration on every call
// rather than held here: an operator lowering a limit because a partner
// complained should see it take effect on the next request, not after a
// restart. A perMinute of zero or less means no limit.
//
// The returned duration is how long to wait before asking again. It is a wait
// and not a refusal because being over a quota is a normal condition: the work
// is still wanted, just not yet.
func (g *Group) Take(key string, perMinute float64) (allowed bool, retryAfter time.Duration) {
	if key == "" || perMinute <= 0 {
		return true, 0
	}

	// Asked before the group's lock, because the counter holds its own and
	// taking two locks in two orders in two places is how a deadlock is
	// written.
	//
	// A partner's quota is per minute and this counter's window is a minute, so
	// the total is what the whole installation has spent against it. Refusing
	// here returns the time until the window turns over rather than a token
	// wait, because no local bucket refill will help: the allowance is gone
	// everywhere.
	if shared := g.sharedCounter(); shared != nil {
		now := g.now()
		if shared.Add(key, now) > int64(perMinute) {
			return false, time.Until(now.Truncate(time.Minute).Add(time.Minute)) + time.Millisecond
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now()
	rate := perMinute / 60
	// A burst of one minute's worth, floored at one: a limit of six an hour must
	// still let a single call through rather than rounding down to none.
	burst := perMinute
	if burst < 1 {
		burst = 1
	}

	b, known := g.buckets[key]
	if !known {
		g.evictLocked(now)
		if len(g.buckets) >= g.settings.MaxBuckets {
			return true, 0 // fail open on bookkeeping; see Settings.MaxBuckets
		}
		b = &bucket{tokens: burst, lastFill: now}
		g.buckets[key] = b
	}

	// A changed configuration takes effect here. Tokens already held are capped
	// to the new burst so lowering a limit cannot be outrun by a bucket that was
	// filled under the old one.
	b.perSecond = rate
	b.burst = burst
	b.lastUsed = now

	elapsed := now.Sub(b.lastFill).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * b.perSecond
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
		b.lastFill = now
	}

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}

	// How long until one token exists. Rounded up to the millisecond so a caller
	// that waits exactly this long finds a token rather than missing by a float.
	needed := (1 - b.tokens) / b.perSecond
	wait := time.Duration(needed*float64(time.Second)) + time.Millisecond
	return false, wait
}

// evictLocked drops buckets nothing has drawn from for a while.
func (g *Group) evictLocked(now time.Time) {
	for key, b := range g.buckets {
		if now.Sub(b.lastUsed) > g.settings.IdleTTL {
			delete(g.buckets, key)
		}
	}
}

func (g *Group) sharedCounter() *sharedcount.Counter {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.shared
}
