// Package circuit stops the engine calling a downstream that is already down.
//
// A job pool is a fixed number of workers. When one endpoint starts timing out,
// every instance waiting on it takes a worker and holds it for the full timeout,
// and the pool fills with work that is certain to fail. Nothing else moves —
// the human tasks, the timers and the healthy integrations all queue behind an
// outage they have nothing to do with.
//
// A breaker turns those calls into an immediate failure. The instances still
// fail, and they still retry with backoff, but they do it in microseconds
// instead of thirty seconds each, and the pool stays available for the work that
// can succeed.
package circuit

import (
	"sync"
	"time"
)

// State is what the breaker will do with the next call.
type State int

const (
	// Closed passes calls through. The normal state.
	Closed State = iota
	// Open refuses them: the downstream has failed enough times in a row that
	// trying again now is a worker spent for nothing.
	Open
	// HalfOpen lets exactly one call through to find out whether the downstream
	// has recovered.
	HalfOpen
)

func (s State) String() string {
	switch s {
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

// Settings tune one group of breakers.
type Settings struct {
	// FailureThreshold is how many consecutive failures open the breaker.
	//
	// Consecutive, not a rate: a downstream that fails one call in ten is not
	// down, it is flaky, and a breaker is the wrong tool for flaky.
	FailureThreshold int

	// OpenDuration is how long an open breaker refuses calls before letting one
	// through to test the water.
	OpenDuration time.Duration

	// IdleTTL is how long a breaker with nothing to say is kept.
	//
	// Breakers are keyed by what is being called, and for a plain HTTP service
	// task that is a host taken from a deployed process definition — untrusted
	// input, of unbounded variety. Without eviction the map is a slow memory
	// leak that a hostile definition can drive.
	IdleTTL time.Duration

	// MaxBreakers bounds the map for the same reason.
	//
	// Past the bound, a new key gets no breaker at all and its calls go
	// through: this is an availability optimisation, and failing open on the
	// bookkeeping leaves the engine exactly as it behaved before breakers
	// existed.
	MaxBreakers int
}

// DefaultSettings are tuned for HTTP integrations.
func DefaultSettings() Settings {
	return Settings{
		FailureThreshold: 5,
		OpenDuration:     30 * time.Second,
		IdleTTL:          15 * time.Minute,
		MaxBreakers:      1024,
	}
}

// Group holds one breaker per thing being called.
type Group struct {
	settings Settings
	now      func() time.Time

	mu       sync.Mutex
	breakers map[string]*breaker
}

type breaker struct {
	failures int
	openedAt time.Time
	lastUsed time.Time
	half     bool // a trial call is out
}

// NewGroup returns a group of breakers sharing one set of settings.
func NewGroup(settings Settings) *Group {
	if settings.FailureThreshold <= 0 {
		settings.FailureThreshold = DefaultSettings().FailureThreshold
	}
	if settings.OpenDuration <= 0 {
		settings.OpenDuration = DefaultSettings().OpenDuration
	}
	if settings.IdleTTL <= 0 {
		settings.IdleTTL = DefaultSettings().IdleTTL
	}
	if settings.MaxBreakers <= 0 {
		settings.MaxBreakers = DefaultSettings().MaxBreakers
	}
	return &Group{
		settings: settings,
		now:      time.Now,
		breakers: make(map[string]*breaker),
	}
}

// Allow reports whether a call to key should be made, and in what state.
//
// A key that has never failed has no breaker and no entry: the common case
// costs one map read and allocates nothing.
func (g *Group) Allow(key string) (bool, State) {
	if key == "" {
		return true, Closed
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	b, known := g.breakers[key]
	if !known {
		return true, Closed
	}
	now := g.now()
	b.lastUsed = now

	if b.failures < g.settings.FailureThreshold {
		return true, Closed
	}
	if now.Sub(b.openedAt) < g.settings.OpenDuration {
		return false, Open
	}
	// The cooldown is over. Exactly one call goes through to find out whether
	// the downstream is back; the rest keep waiting, because a recovering
	// service does not need the whole backlog at once.
	if b.half {
		return false, Open
	}
	b.half = true
	return true, HalfOpen
}

// Succeeded records a call that worked, closing the breaker.
func (g *Group) Succeeded(key string) {
	if key == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	// Deleted rather than reset: a healthy downstream should cost nothing to
	// remember, and this is what keeps the map proportional to the number of
	// things currently unhealthy rather than to the number ever called.
	delete(g.breakers, key)
}

// Failed records a call that did not, and opens the breaker once there have
// been enough in a row.
func (g *Group) Failed(key string) {
	if key == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now()
	b, known := g.breakers[key]
	if !known {
		g.evictLocked(now)
		if len(g.breakers) >= g.settings.MaxBreakers {
			return // fail open on bookkeeping; see Settings.MaxBreakers
		}
		b = &breaker{}
		g.breakers[key] = b
	}

	b.failures++
	b.lastUsed = now
	b.half = false
	if b.failures >= g.settings.FailureThreshold {
		// Re-stamped on every failure past the threshold, so a downstream that
		// keeps failing its trial call keeps its full cooldown.
		b.openedAt = now
	}
}

// State reports what the breaker would do, without touching it.
func (g *Group) State(key string) State {
	g.mu.Lock()
	defer g.mu.Unlock()

	b, known := g.breakers[key]
	if !known || b.failures < g.settings.FailureThreshold {
		return Closed
	}
	if g.now().Sub(b.openedAt) < g.settings.OpenDuration {
		return Open
	}
	return HalfOpen
}

// evictLocked drops breakers nothing has asked about for a while.
func (g *Group) evictLocked(now time.Time) {
	for key, b := range g.breakers {
		if now.Sub(b.lastUsed) > g.settings.IdleTTL {
			delete(g.breakers, key)
		}
	}
}
