package ratelimit

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

type fixedClock struct{ at time.Time }

func (c *fixedClock) now() time.Time          { return c.at }
func (c *fixedClock) advance(d time.Duration) { c.at = c.at.Add(d) }

func newTestGroup(t *testing.T, settings Settings) (*Group, *fixedClock) {
	t.Helper()
	clock := &fixedClock{at: time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)}
	g := NewGroup(settings)
	g.now = clock.now
	return g, clock
}

// The quota shape APIs are actually written in: a burst allowance that refills
// at a steady rate.
func TestABurstIsAllowedThenTheRateTakesOver(t *testing.T) {
	g, clock := newTestGroup(t, DefaultSettings())

	// Sixty a minute is one a second with a minute's burst.
	for i := range 60 {
		if allowed, _ := g.Take("api", 60); !allowed {
			t.Fatalf("call %d was limited inside the burst", i+1)
		}
	}

	allowed, wait := g.Take("api", 60)
	if allowed {
		t.Error("the 61st call went through with no time passing")
	}
	if wait <= 0 || wait > 2*time.Second {
		t.Errorf("wait = %v, want about a second", wait)
	}

	clock.advance(wait)
	if allowed, _ := g.Take("api", 60); !allowed {
		t.Error("no token after waiting exactly as long as we were told to")
	}
}

// The answer is a wait, not a refusal: the work is still wanted, just not yet.
func TestTheWaitIsLongEnoughToBeWorthTaking(t *testing.T) {
	g, clock := newTestGroup(t, DefaultSettings())

	// Six an hour: one every ten minutes, burst of one.
	if allowed, _ := g.Take("slow", 6.0/60); !allowed {
		t.Fatal("the first call was limited; a burst below one must still allow one")
	}
	allowed, wait := g.Take("slow", 6.0/60)
	if allowed {
		t.Fatal("a second call went through immediately against a six-an-hour limit")
	}
	if wait < 9*time.Minute || wait > 11*time.Minute {
		t.Errorf("wait = %v, want about ten minutes", wait)
	}

	clock.advance(wait)
	if allowed, _ := g.Take("slow", 6.0/60); !allowed {
		t.Error("still limited after waiting the full interval")
	}
}

// Targets do not share a quota.
func TestEachTargetHasItsOwnQuota(t *testing.T) {
	g, _ := newTestGroup(t, DefaultSettings())

	for range 60 {
		g.Take("a", 60)
	}
	if allowed, _ := g.Take("a", 60); allowed {
		t.Fatal("target a was not limited")
	}
	if allowed, _ := g.Take("b", 60); !allowed {
		t.Error("target b was limited by target a's traffic")
	}
}

// An operator lowering a limit because a partner complained should see it take
// effect on the next call, not after a restart — and must not be outrun by a
// bucket filled under the old limit.
func TestLoweringALimitTakesEffectImmediately(t *testing.T) {
	g, clock := newTestGroup(t, DefaultSettings())

	if allowed, _ := g.Take("api", 600); !allowed {
		t.Fatal("the first call was limited")
	}
	clock.advance(time.Minute) // fill right up under the old limit

	// Now the limit is one a minute. The burst is one, so one call goes through
	// and the next does not.
	if allowed, _ := g.Take("api", 1); !allowed {
		t.Fatal("the first call under the new limit was refused")
	}
	if allowed, _ := g.Take("api", 1); allowed {
		t.Error("a bucket filled under the old limit outran the new one")
	}
}

// No limit configured means no limiting, which is what every existing
// installation has.
func TestNoLimitMeansNoLimiting(t *testing.T) {
	g, _ := newTestGroup(t, DefaultSettings())

	for i := range 1000 {
		if allowed, _ := g.Take("api", 0); !allowed {
			t.Fatalf("call %d was limited with no limit configured", i+1)
		}
	}
	g.mu.Lock()
	size := len(g.buckets)
	g.mu.Unlock()
	if size != 0 {
		t.Errorf("an unlimited target created %d buckets", size)
	}
}

// Keyed by a host from a deployed definition, so the map is bounded and forgets.
func TestTheMapIsBoundedAndForgets(t *testing.T) {
	g, clock := newTestGroup(t, Settings{IdleTTL: 10 * time.Minute, MaxBuckets: 8})

	for i := range 100 {
		g.Take("host-"+strconv.Itoa(i), 60)
	}
	g.mu.Lock()
	size := len(g.buckets)
	g.mu.Unlock()
	if size > 8 {
		t.Errorf("the map holds %d buckets, past the bound of 8", size)
	}

	// Past the bound a call is allowed rather than refused: this protects a
	// partner's quota, not the engine's integrity.
	if allowed, _ := g.Take("host-99", 60); !allowed {
		t.Error("a call was refused by bookkeeping rather than by a quota")
	}

	clock.advance(11 * time.Minute)
	g.Take("fresh", 60)
	g.mu.Lock()
	_, made := g.buckets["fresh"]
	g.mu.Unlock()
	if !made {
		t.Error("no bucket was made after the idle ones were evicted")
	}
}

func TestConcurrentUse(t *testing.T) {
	g, _ := newTestGroup(t, DefaultSettings())

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "api-" + strconv.Itoa(n%4)
			for range 200 {
				g.Take(key, 600)
			}
		}(i)
	}
	wg.Wait()
}
