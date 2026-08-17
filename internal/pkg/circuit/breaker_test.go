package circuit

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

// fixedClock lets the tests move time rather than wait for it. A breaker's whole
// behaviour is about elapsed time, and a test that sleeps for it is a test that
// is slow and flaky at the same time.
type fixedClock struct{ at time.Time }

func (c *fixedClock) now() time.Time          { return c.at }
func (c *fixedClock) advance(d time.Duration) { c.at = c.at.Add(d) }

func newTestGroup(t *testing.T, settings Settings) (*Group, *fixedClock) {
	t.Helper()
	clock := &fixedClock{at: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)}
	g := NewGroup(settings)
	g.now = clock.now
	return g, clock
}

// The reason breakers exist. A downstream that has failed enough times in a row
// stops taking a worker per instance for the full timeout.
func TestABrokenDownstreamStopsBeingCalled(t *testing.T) {
	g, _ := newTestGroup(t, Settings{FailureThreshold: 3, OpenDuration: time.Minute})

	for i := 0; i < 2; i++ {
		if allowed, _ := g.Allow("api"); !allowed {
			t.Fatalf("refused after %d failures; the threshold is 3", i)
		}
		g.Failed("api")
	}

	if allowed, _ := g.Allow("api"); !allowed {
		t.Fatal("refused after 2 failures; the threshold is 3")
	}
	g.Failed("api")

	allowed, state := g.Allow("api")
	if allowed {
		t.Error("a third consecutive failure did not open the breaker")
	}
	if state != Open {
		t.Errorf("state = %v, want open", state)
	}
}

// Consecutive, not a rate: an endpoint that fails one call in ten is flaky, and
// refusing all of its traffic would be a worse outage than the one it is having.
func TestASuccessClearsTheCount(t *testing.T) {
	g, _ := newTestGroup(t, Settings{FailureThreshold: 3, OpenDuration: time.Minute})

	g.Failed("api")
	g.Failed("api")
	g.Succeeded("api")
	g.Failed("api")
	g.Failed("api")

	if allowed, _ := g.Allow("api"); !allowed {
		t.Error("the breaker opened on four failures split by a success; they must be consecutive")
	}
}

// After the cooldown one call goes through to find out whether the downstream is
// back — and only one, because a recovering service does not need the whole
// backlog at once.
func TestExactlyOneCallTestsTheWater(t *testing.T) {
	g, clock := newTestGroup(t, Settings{FailureThreshold: 1, OpenDuration: time.Minute})

	g.Failed("api")
	if allowed, _ := g.Allow("api"); allowed {
		t.Fatal("the breaker did not open")
	}

	clock.advance(time.Minute + time.Second)

	allowed, state := g.Allow("api")
	if !allowed || state != HalfOpen {
		t.Fatalf("after the cooldown: allowed=%v state=%v, want one call through, half-open", allowed, state)
	}
	if second, _ := g.Allow("api"); second {
		t.Error("a second call went through while the first was still out")
	}
}

func TestARecoveredDownstreamIsCalledAgain(t *testing.T) {
	g, clock := newTestGroup(t, Settings{FailureThreshold: 1, OpenDuration: time.Minute})

	g.Failed("api")
	clock.advance(2 * time.Minute)

	if allowed, _ := g.Allow("api"); !allowed {
		t.Fatal("no trial call after the cooldown")
	}
	g.Succeeded("api")

	if allowed, state := g.Allow("api"); !allowed || state != Closed {
		t.Errorf("after a successful trial: allowed=%v state=%v, want closed", allowed, state)
	}
}

// A downstream that fails its trial call gets the full cooldown again, rather
// than a trial call on every attempt from then on.
func TestAFailedTrialReopensForTheFullCooldown(t *testing.T) {
	g, clock := newTestGroup(t, Settings{FailureThreshold: 1, OpenDuration: time.Minute})

	g.Failed("api")
	clock.advance(2 * time.Minute)
	if allowed, _ := g.Allow("api"); !allowed {
		t.Fatal("no trial call after the cooldown")
	}
	g.Failed("api")

	clock.advance(30 * time.Second)
	if allowed, _ := g.Allow("api"); allowed {
		t.Error("a call went through 30 seconds into a one-minute cooldown")
	}
	clock.advance(31 * time.Second)
	if allowed, _ := g.Allow("api"); !allowed {
		t.Error("no trial call after the second cooldown")
	}
}

// Breakers are keyed by what is being called, and for a plain HTTP service task
// that is a host from a deployed process definition — untrusted input of
// unbounded variety. The map has to be bounded and has to forget.
func TestTheMapIsBoundedAndForgets(t *testing.T) {
	g, clock := newTestGroup(t, Settings{
		FailureThreshold: 1,
		OpenDuration:     time.Minute,
		IdleTTL:          10 * time.Minute,
		MaxBreakers:      8,
	})

	for i := 0; i < 100; i++ {
		g.Failed("host-" + strconv.Itoa(i))
	}
	g.mu.Lock()
	size := len(g.breakers)
	g.mu.Unlock()
	if size > 8 {
		t.Errorf("the map holds %d breakers, past the bound of 8", size)
	}

	// Past the bound a new key gets no breaker and its calls go through: this is
	// an availability optimisation, and failing open on the bookkeeping leaves
	// the engine as it behaved before breakers existed.
	if allowed, _ := g.Allow("host-99"); !allowed {
		t.Error("a call was refused by bookkeeping rather than by a downstream")
	}

	// Once the old ones go idle, the space comes back.
	clock.advance(11 * time.Minute)
	g.Failed("fresh")
	if allowed, _ := g.Allow("fresh"); allowed {
		t.Error("the fresh breaker was not created after the idle ones were evicted")
	}
}

// A key nothing has ever failed on costs one map read and holds no memory.
func TestAHealthyDownstreamIsNotRemembered(t *testing.T) {
	g, _ := newTestGroup(t, DefaultSettings())

	for i := 0; i < 1000; i++ {
		if allowed, _ := g.Allow("api"); !allowed {
			t.Fatal("a healthy downstream was refused")
		}
		g.Succeeded("api")
	}

	g.mu.Lock()
	size := len(g.breakers)
	g.mu.Unlock()
	if size != 0 {
		t.Errorf("the map holds %d entries for a downstream that never failed", size)
	}
}

// Jobs run concurrently, so every entry point is reached from several
// goroutines at once.
func TestConcurrentUse(t *testing.T) {
	g, _ := newTestGroup(t, DefaultSettings())

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "api-" + strconv.Itoa(n%4)
			for j := 0; j < 200; j++ {
				g.Allow(key)
				if j%3 == 0 {
					g.Failed(key)
				} else {
					g.Succeeded(key)
				}
				g.State(key)
			}
		}(i)
	}
	wg.Wait()
}

// An empty key is a node with nothing to call, and must be a no-op rather than
// one shared breaker every such node trips for the others.
func TestAnEmptyKeyIsANoOp(t *testing.T) {
	g, _ := newTestGroup(t, Settings{FailureThreshold: 1})

	for i := 0; i < 10; i++ {
		g.Failed("")
	}
	if allowed, _ := g.Allow(""); !allowed {
		t.Error("an empty key was refused")
	}
	g.mu.Lock()
	size := len(g.breakers)
	g.mu.Unlock()
	if size != 0 {
		t.Errorf("an empty key created %d breakers", size)
	}
}
