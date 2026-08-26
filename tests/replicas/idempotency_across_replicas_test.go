// Package replicas proves the properties that only break when more than one
// process serves the same database.
//
// They cannot be observed in a single-process test by construction: an
// in-memory cache is perfectly correct until a second replica exists, which is
// why the idempotency cache shipped unsafe for multi-replica deployments while
// every test passed. Here each "replica" is an independent interceptor over one
// shared database — the same thing two pods are.
package replicas

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gsoultan/gobpm/server/interceptors/security"
	"github.com/gsoultan/gobpm/tests/testutils"
	"gorm.io/gorm"
)

const idempotencyTTL = time.Minute

// testMaxConns keeps the server-backed pools small; these tests are short and a
// wide pool only slows schema setup.
const testMaxConns = 4

// forEachDialect runs body against every SQL engine the product supports.
//
// The claim is a conditional INSERT, and "conditional" is spelled differently
// on each engine — ON CONFLICT DO NOTHING, INSERT IGNORE, or a duplicate-key
// error the store has to recognise. A claim that silently stopped being atomic
// on one dialect would let two replicas execute the same write there and
// nowhere else, so each is proven rather than assumed.
func forEachDialect(t *testing.T, body func(t *testing.T, db *gorm.DB)) {
	t.Helper()
	engines := []struct {
		name string
		open func(*testing.T) *gorm.DB
	}{
		{"sqlite", func(t *testing.T) *gorm.DB { return testutils.SetupTestDB(t) }},
		{"postgres", func(t *testing.T) *gorm.DB { return testutils.SetupPostgresDB(t, testMaxConns) }},
		{"mysql", func(t *testing.T) *gorm.DB { return testutils.SetupMySQLDB(t, testMaxConns) }},
	}
	for _, engine := range engines {
		t.Run(engine.name, func(t *testing.T) {
			body(t, engine.open(t))
		})
	}
}

// replica is one server process: its own interceptor, over the shared database.
type replica struct {
	handler http.Handler
}

func newReplica(t *testing.T, db *gorm.DB, calls *atomic.Int32, body func(w http.ResponseWriter, seq int32)) replica {
	t.Helper()
	interceptor := security.NewIdempotencyInterceptorWithStore(
		security.NewDBIdempotencyStore(db, idempotencyTTL), idempotencyTTL)

	return replica{handler: interceptor.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		body(w, calls.Add(1))
	}))}
}

func post(t *testing.T, r replica, key, payload string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/process/start", bytes.NewReader([]byte(payload)))
	req.Header.Set("Idempotency-Key", key)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.handler.ServeHTTP(rec, req)
	return rec
}

// TestARetryOnAnotherReplicaDoesNotExecuteTwice is the whole point.
//
// With the cache in the serving process, replica B saw an empty map and ran the
// write a second time — a duplicate business action, which for this system can
// mean charging a card twice.
func TestARetryOnAnotherReplicaDoesNotExecuteTwice(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		var calls atomic.Int32

		body := func(w http.ResponseWriter, seq int32) {
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"instance":%d}`, seq)
		}
		replicaA := newReplica(t, db, &calls, body)
		replicaB := newReplica(t, db, &calls, body)

		first := post(t, replicaA, "order-4471", `{"definition_key":"invoice"}`)
		if first.Code != http.StatusCreated {
			t.Fatalf("first request: %d", first.Code)
		}

		second := post(t, replicaB, "order-4471", `{"definition_key":"invoice"}`)

		if got := calls.Load(); got != 1 {
			t.Errorf("the handler ran %d times across two replicas, want 1 — the retry executed the write again", got)
		}
		if second.Body.String() != first.Body.String() {
			t.Errorf("replica B answered %q, want replica A's original %q", second.Body.String(), first.Body.String())
		}
		if second.Header().Get("Idempotency-Replayed") != "true" {
			t.Error("replica B did not mark its answer as a replay")
		}
	})
}

// TestKeyReuseIsRefusedAcrossReplicas keeps the conflict check working when the
// two requests land on different processes — a client bug must not become
// invisible because a load balancer moved it.
func TestKeyReuseIsRefusedAcrossReplicas(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		var calls atomic.Int32
		body := func(w http.ResponseWriter, _ int32) { w.WriteHeader(http.StatusCreated) }

		replicaA := newReplica(t, db, &calls, body)
		replicaB := newReplica(t, db, &calls, body)

		post(t, replicaA, "order-4471", `{"definition_key":"invoice"}`)
		second := post(t, replicaB, "order-4471", `{"definition_key":"something-else"}`)

		if second.Code != http.StatusConflict {
			t.Fatalf("reusing a key for a different payload on another replica returned %d, want 409", second.Code)
		}
	})
}

// TestConcurrentReplicasElectExactlyOneExecutor covers the race the claim
// exists for: several replicas receive the request at once, none has seen the
// key, and exactly one must run it. A check-then-insert would let them all
// through.
func TestConcurrentReplicasElectExactlyOneExecutor(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		var calls atomic.Int32

		// Slow enough that the other claims genuinely overlap the first.
		body := func(w http.ResponseWriter, seq int32) {
			time.Sleep(150 * time.Millisecond)
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"instance":%d}`, seq)
		}

		const replicaCount = 4
		replicas := make([]replica, replicaCount)
		for i := range replicas {
			replicas[i] = newReplica(t, db, &calls, body)
		}

		start := make(chan struct{})
		bodies := make([]string, replicaCount)
		codes := make([]int, replicaCount)
		var wg sync.WaitGroup
		for i, r := range replicas {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				rec := post(t, r, "order-9001", `{"definition_key":"invoice"}`)
				bodies[i], codes[i] = rec.Body.String(), rec.Code
			}()
		}
		close(start)
		wg.Wait()

		if got := calls.Load(); got != 1 {
			t.Fatalf("the handler ran %d times for one key across %d concurrent replicas, want 1", got, replicaCount)
		}
		for i := range replicas {
			if codes[i] != http.StatusCreated {
				t.Errorf("replica %d answered %d, want 201 — it waited but never received the original response", i, codes[i])
			}
			if bodies[i] != bodies[0] {
				t.Errorf("replica %d answered %q while replica 0 answered %q; one key produced two answers", i, bodies[i], bodies[0])
			}
		}
	})
}

// TestAnAbandonedClaimDoesNotStrandRetries covers the failure case: a replica
// takes the key and dies without recording anything. Without releasing the
// claim, every retry waits out the budget for an answer nobody will write.
func TestAnAbandonedClaimDoesNotStrandRetries(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		var deadCalls atomic.Int32

		panicking := newReplica(t, db, &deadCalls, func(http.ResponseWriter, int32) {
			panic("the process died mid-request")
		})

		func() {
			defer func() { _ = recover() }()
			post(t, panicking, "order-doomed", `{"definition_key":"invoice"}`)
		}()

		var healthyCalls atomic.Int32
		healthy := newReplica(t, db, &healthyCalls, func(w http.ResponseWriter, seq int32) {
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"instance":%d}`, seq)
		})

		began := time.Now()
		retry := post(t, healthy, "order-doomed", `{"definition_key":"invoice"}`)
		elapsed := time.Since(began)

		if retry.Code != http.StatusCreated {
			t.Fatalf("the retry after an abandoned claim returned %d, want 201", retry.Code)
		}
		if healthyCalls.Load() != 1 {
			t.Fatal("the retry did not execute; the abandoned claim stranded it")
		}
		if elapsed > 3*time.Second {
			t.Fatalf("the retry waited %v on a claim nobody would complete", elapsed)
		}
	})
}
