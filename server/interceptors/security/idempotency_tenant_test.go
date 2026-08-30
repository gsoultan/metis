package security

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
)

// An Idempotency-Key is chosen by the client, and clients choose obvious
// things: an order number, a business reference, "1". So the key alone does not
// identify a request — the caller does too.
//
// Keyed on method, path and header alone, two tenants picking the same value
// met in one cache entry. The second was handed the first's response body and
// their own write never ran: a cross-tenant disclosure and a silently dropped
// command in the same exchange. AGENTS §2.3 names this shape directly — a cache
// that ignores who asked is a data leak.

// echoingHandler reports which call it is and who it ran as, so a replay is
// distinguishable from a fresh execution by body alone.
func echoingHandler(calls *atomic.Int32) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		tc, _ := entities.TenantContextFrom(r.Context())
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(w, `{"call":%d,"org":%q}`, n, tc.TenantID)
	})
}

type caller struct {
	user uuid.UUID
	org  string
}

func (c caller) request(t *testing.T, key, body string) *http.Request {
	t.Helper()
	ctx := t.Context()
	if c.user != uuid.Nil {
		ctx = context.WithValue(ctx, pkgauth.UserContextKey, entities.User{ID: c.user, Username: c.user.String()})
	}
	if c.org != "" {
		ctx = entities.WithTenantContext(ctx, entities.TenantContext{TenantID: c.org})
	}
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/process/start", bytes.NewReader([]byte(body)))
	r.Header.Set(idempotencyKeyHeader, key)
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestIdempotencyIsScopedToTheCaller(t *testing.T) {
	alice := caller{user: uuid.Must(uuid.NewV7()), org: "org-a"}
	bob := caller{user: uuid.Must(uuid.NewV7()), org: "org-b"}
	carol := caller{user: uuid.Must(uuid.NewV7()), org: "org-a"} // same org as alice

	const key = "order-1"
	const body = `{"definition_key":"invoice"}`

	testCases := []struct {
		name        string
		first       caller
		second      caller
		wantReplay  bool
		wantHandler int32
		why         string
	}{
		{
			name: "different tenants never share an entry", first: alice, second: bob,
			wantReplay: false, wantHandler: 2,
			why: "org-b would be served org-a's response and its own command would never run",
		},
		{
			name: "different users in one tenant are different commands", first: alice, second: carol,
			wantReplay: false, wantHandler: 2,
			why: "two people retrying with the same obvious key are not one another's retry",
		},
		{
			name: "the same caller retrying is a replay", first: alice, second: alice,
			wantReplay: true, wantHandler: 1,
			why: "this is what the header is for; scoping must not defeat it",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			handler := NewIdempotencyInterceptor(time.Minute).Wrap(echoingHandler(&calls))

			firstResponse := httptest.NewRecorder()
			handler.ServeHTTP(firstResponse, tc.first.request(t, key, body))

			secondResponse := httptest.NewRecorder()
			handler.ServeHTTP(secondResponse, tc.second.request(t, key, body))

			replayed := secondResponse.Header().Get(idempotencyReplayHeader) == "true"
			if replayed != tc.wantReplay {
				t.Errorf("replayed=%v, want %v — %s\nfirst:  %s\nsecond: %s",
					replayed, tc.wantReplay, tc.why, firstResponse.Body.String(), secondResponse.Body.String())
			}
			if got := calls.Load(); got != tc.wantHandler {
				t.Errorf("handler ran %d times, want %d — %s", got, tc.wantHandler, tc.why)
			}
		})
	}
}

// TestIdempotencyKeyReuseIsStillRefusedWithinOneCaller keeps the original
// guarantee: scoping widened the key, it did not weaken the conflict check that
// catches a client reusing one key for two different payloads.
func TestIdempotencyKeyReuseIsStillRefusedWithinOneCaller(t *testing.T) {
	alice := caller{user: uuid.Must(uuid.NewV7()), org: "org-a"}
	var calls atomic.Int32
	handler := NewIdempotencyInterceptor(time.Minute).Wrap(echoingHandler(&calls))

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, alice.request(t, "order-1", `{"definition_key":"invoice"}`))

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, alice.request(t, "order-1", `{"definition_key":"something-else"}`))

	if second.Code != idempotencyConflictStatusCode {
		t.Fatalf("reusing one key for a different payload returned %d, want %d",
			second.Code, idempotencyConflictStatusCode)
	}
}

// TestIdempotencyStillWorksForUnauthenticatedCallers pins that the interceptor
// does not refuse work merely because no principal is present — it sits in
// front of paths that have none, and an empty identity is its own bucket rather
// than a shared one.
func TestIdempotencyStillWorksForUnauthenticatedCallers(t *testing.T) {
	anonymous := caller{}
	var calls atomic.Int32
	handler := NewIdempotencyInterceptor(time.Minute).Wrap(echoingHandler(&calls))

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, anonymous.request(t, "order-1", `{"a":1}`))
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, anonymous.request(t, "order-1", `{"a":1}`))

	if second.Header().Get(idempotencyReplayHeader) != "true" {
		t.Fatal("an anonymous caller's retry was not replayed")
	}
	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times for one anonymous retry, want 1", calls.Load())
	}
}
