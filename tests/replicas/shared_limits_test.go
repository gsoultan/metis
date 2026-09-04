package replicas

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gsoultan/metis/internal/pkg/sharedcount"
	"github.com/gsoultan/metis/server/interceptors/security"
	"github.com/gsoultan/metis/server/repositories"
	"gorm.io/gorm"
)

// A rate limit has to be the installation's, not each process's.
//
// Held in memory, N replicas each admit the whole limit, so the installation
// admits N times what was configured — and a partner's quota is spent N times
// over. That is the specific cost that made a single replica the supported
// topology.
func TestALimitIsSharedRatherThanAppliedTwice(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		const limit = 10
		repo := repositories.NewRepository(db)
		now := time.Now()

		a := sharedcount.New(repo.SharedCounter(), "http-rate", "replica-a", time.Minute)
		b := sharedcount.New(repo.SharedCounter(), "http-rate", "replica-b", time.Minute)

		// The load balancer sends half of one client's traffic to each replica.
		admitted := 0
		for i := range 40 {
			counter := a
			if i%2 == 1 {
				counter = b
			}
			if counter.Add("198.51.100.10", now) <= limit {
				admitted++
			}
			// The replicas exchange as they go, the way the timer does.
			if i%4 == 3 {
				a.Exchange(t.Context())
				b.Exchange(t.Context())
			}
		}

		// Not exact: between exchanges a replica does not know what the other
		// has counted, so the installation overshoots by up to one interval's
		// worth per replica. Bounded overshoot is the trade — what it replaces
		// is N times the limit, permanently.
		// Two replicas each admitting their own allowance is exactly 2*limit,
		// so that has to fail rather than sit on the boundary. It sat on the
		// boundary on the first run against MySQL, where the shared read was a
		// syntax error and every exchange silently did nothing — a tolerance
		// that admits the unshared answer tests the tolerance, not the sharing.
		if admitted >= 2*limit {
			t.Errorf("admitted %d against a limit of %d: %d is what two replicas admit when they share nothing", admitted, limit, 2*limit)
		}
		if admitted < limit {
			t.Errorf("admitted %d against a limit of %d: the shared count is refusing traffic the limit allows", admitted, limit)
		}
		t.Logf("admitted %d of 40 against a limit of %d, across two replicas", admitted, limit)
	})
}

// The totals have to survive the round trip through the real dialect: the row
// is keyed by replica so two processes never write the same one, and the shared
// value is a SUM at read time.
func TestEachReplicaOwnsItsOwnRow(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		repo := repositories.NewRepository(db).SharedCounter()
		window := time.Now().Truncate(time.Minute)
		ctx := t.Context()

		if err := repo.Record(ctx, "http-rate", "client", "replica-a", window, 7); err != nil {
			t.Fatalf("record for replica A: %v", err)
		}
		if err := repo.Record(ctx, "http-rate", "client", "replica-b", window, 5); err != nil {
			t.Fatalf("record for replica B: %v", err)
		}
		// A replica rewriting its own row replaces it rather than adding to it:
		// the value is absolute, which is what makes a lost update impossible.
		if err := repo.Record(ctx, "http-rate", "client", "replica-a", window, 9); err != nil {
			t.Fatalf("re-record for replica A: %v", err)
		}

		totals, err := repo.Totals(ctx, "http-rate", []string{"client"}, window)
		if err != nil {
			t.Fatalf("totals: %v", err)
		}
		if totals["client"] != 14 {
			t.Fatalf("total is %d, want 14 (replica A's latest 9 plus replica B's 5)", totals["client"])
		}
	})
}

// A different scope must not collide with another's keys.
func TestScopesDoNotShareAKeySpace(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		repo := repositories.NewRepository(db).SharedCounter()
		window := time.Now().Truncate(time.Minute)
		ctx := t.Context()

		if err := repo.Record(ctx, "http-rate", "same-key", "replica-a", window, 100); err != nil {
			t.Fatalf("record: %v", err)
		}
		if err := repo.Record(ctx, "connector-rate", "same-key", "replica-a", window, 3); err != nil {
			t.Fatalf("record: %v", err)
		}

		totals, err := repo.Totals(ctx, "connector-rate", []string{"same-key"}, window)
		if err != nil {
			t.Fatalf("totals: %v", err)
		}
		if totals["same-key"] != 3 {
			t.Fatalf("connector scope reads %d, want 3: an inbound client address and a connector target are colliding", totals["same-key"])
		}
	})
}

// Closed windows are removed, or the table grows with every client that has
// ever been seen.
func TestClosedWindowsArePruned(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		repo := repositories.NewRepository(db).SharedCounter()
		ctx := t.Context()
		old := time.Now().Add(-2 * time.Hour).Truncate(time.Minute)

		if err := repo.Record(ctx, "http-rate", "client", "replica-a", old, 1); err != nil {
			t.Fatalf("record: %v", err)
		}
		removed, err := repo.Prune(ctx, time.Now().Add(-time.Hour))
		if err != nil {
			t.Fatalf("prune: %v", err)
		}
		if removed == 0 {
			t.Fatal("the prune removed nothing, so the table grows without bound")
		}
	})
}

// The interceptor has to actually be given the counter. Building the plumbing
// and not connecting it leaves a limit that is quietly per-process and looks
// exactly like one that is not — which is what happened on the first attempt.
func TestTheInterceptorAcceptsASharedCounter(t *testing.T) {
	limiter := security.NewRateLimitInterceptor(1, time.Minute)

	sharer, ok := limiter.(security.SharedLimiter)
	if !ok {
		t.Fatal("the rate limiter cannot be given a shared counter, so the limit can only ever be per-process")
	}

	// With a counter whose total is already past the limit, the very first
	// request must be refused — proving the shared count is consulted rather
	// than only the local window.
	counter := sharedcount.New(alwaysOverLimit{}, "http-rate", "replica-a", time.Minute)
	counter.Exchange(t.Context())
	sharer.ShareVia(counter)

	handler := limiter.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	allowed := 0
	for range 5 {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/tasks", nil)
		req.RemoteAddr = "198.51.100.10:4444"
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code == http.StatusOK {
			allowed++
		}
	}
	if allowed > 1 {
		t.Fatalf("%d requests were allowed against a limit of 1: the shared count is not being consulted", allowed)
	}
}

// alwaysOverLimit reports that other replicas have already spent the allowance,
// which is the state a replica joining a busy installation finds itself in.
type alwaysOverLimit struct{}

func (alwaysOverLimit) Record(context.Context, string, string, string, time.Time, int64) error {
	return nil
}

func (alwaysOverLimit) Totals(_ context.Context, _ string, keys []string, _ time.Time) (map[string]int64, error) {
	totals := make(map[string]int64, len(keys))
	for _, key := range keys {
		totals[key] = 1_000_000
	}
	return totals, nil
}

func (alwaysOverLimit) Prune(context.Context, time.Time) (int64, error) { return 0, nil }
