package sharedcount

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/repositories/models"
)

// memoryStore stands in for the table: rows keyed by scope, key, replica and
// window, exactly as the primary key is.
type memoryStore struct {
	mu   sync.Mutex
	rows map[string]int64
	fail bool
}

func newStore() *memoryStore { return &memoryStore{rows: map[string]int64{}} }

func (s *memoryStore) key(scope, key, replica string, window time.Time) string {
	return scope + "|" + key + "|" + replica + "|" + window.UTC().Format(time.RFC3339Nano)
}

func (s *memoryStore) Record(_ context.Context, scope, key, replica string, window time.Time, count int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return context.DeadlineExceeded
	}
	s.rows[s.key(scope, key, replica, window)] = count
	return nil
}

func (s *memoryStore) Totals(_ context.Context, scope string, keys []string, window time.Time) (map[string]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return nil, context.DeadlineExceeded
	}
	totals := map[string]int64{}
	for _, key := range keys {
		prefix := scope + "|" + key + "|"
		suffix := "|" + window.UTC().Format(time.RFC3339Nano)
		for stored, count := range s.rows {
			if len(stored) > len(prefix) && stored[:len(prefix)] == prefix &&
				stored[len(stored)-len(suffix):] == suffix {
				totals[key] += count
			}
		}
	}
	return totals, nil
}

func (s *memoryStore) Prune(context.Context, time.Time) (int64, error) { return 0, nil }

var _ = models.SharedCounterModel{}

const window = time.Minute

// Two replicas counting the same client must add up. Per-process counters do
// not: each sees half the traffic and admits the whole limit, so the
// installation admits twice what was configured.
func TestTwoReplicasSeeEachOthersCounts(t *testing.T) {
	store := newStore()
	now := time.Now()

	a := New(store, "http", "replica-a", window)
	b := New(store, "http", "replica-b", window)

	for range 30 {
		a.Add("198.51.100.10", now)
	}
	for range 20 {
		b.Add("198.51.100.10", now)
	}

	// Before exchanging, each replica knows only its own share — which is
	// exactly the bug being fixed.
	if got := a.Total("198.51.100.10", now); got != 30 {
		t.Fatalf("before the exchange replica A saw %d, want its own 30", got)
	}

	a.Exchange(t.Context())
	b.Exchange(t.Context())
	a.Exchange(t.Context())

	if got := a.Total("198.51.100.10", now); got != 50 {
		t.Errorf("replica A sees %d after exchanging, want the installation's 50", got)
	}
	if got := b.Total("198.51.100.10", now); got != 50 {
		t.Errorf("replica B sees %d after exchanging, want the installation's 50", got)
	}
}

// A replica must not count its own contribution twice when it reads back a
// total that already includes it.
func TestARepeatedExchangeDoesNotInflateTheTotal(t *testing.T) {
	store := newStore()
	now := time.Now()
	counter := New(store, "http", "replica-a", window)

	for range 10 {
		counter.Add("client", now)
	}
	for range 5 {
		counter.Exchange(t.Context())
	}

	if got := counter.Total("client", now); got != 10 {
		t.Fatalf("after five exchanges the count is %d, want 10: the replica is counting itself", got)
	}
}

// A closed window's traffic must not be charged to an open one.
func TestANewWindowStartsFromNothing(t *testing.T) {
	store := newStore()
	base := time.Now().Truncate(window)
	counter := New(store, "http", "replica-a", window)

	for range 40 {
		counter.Add("client", base)
	}
	counter.Exchange(t.Context())

	next := base.Add(window)
	if got := counter.Total("client", next); got != 0 {
		t.Fatalf("a new window opened holding %d, want 0", got)
	}
	if got := counter.Add("client", next); got != 1 {
		t.Fatalf("the first request of a new window counted as %d, want 1", got)
	}
}

// A database that will not answer must degrade to the old behaviour rather than
// to no limit at all. Enforcing your own share is what shipped before this
// existed; enforcing nothing is a worse failure than the one being fixed.
func TestAFailedExchangeStillEnforcesTheLocalCount(t *testing.T) {
	store := newStore()
	store.fail = true
	now := time.Now()
	counter := New(store, "http", "replica-a", window)

	for range 25 {
		counter.Add("client", now)
	}
	counter.Exchange(t.Context())

	if got := counter.Total("client", now); got != 25 {
		t.Fatalf("with the store failing the replica sees %d, want its own 25", got)
	}
}

// The count is read on every request, so it has to be safe to.
func TestConcurrentAddsAreCountedOnce(t *testing.T) {
	store := newStore()
	now := time.Now()
	counter := New(store, "http", "replica-a", window)

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter.Add("client", now)
		}()
	}
	wg.Wait()

	if got := counter.Total("client", now); got != 100 {
		t.Fatalf("100 concurrent requests counted as %d", got)
	}
}
