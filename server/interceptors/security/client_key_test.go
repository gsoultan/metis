package security

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The rightmost hop used to be returned verbatim when it did not parse, and the
// result was the bucket key. So a client behind any trusted proxy could write
// a new string into the header on every request — up to the header limit —
// and be given a new allowance, and a new entry in the shared counter, each
// time.
func TestAGarbageForwardedHopCannotBuyAFreshBucket(t *testing.T) {
	t.Parallel()

	const limit = 3
	interceptor := NewRateLimitInterceptor(limit, time.Minute)
	handler := interceptor.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	allowed := 0
	for i := range 50 {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/tasks", nil)
		req.RemoteAddr = "10.0.0.1:443" // a trusted proxy
		req.Header.Set("X-Forwarded-For", strings.Repeat("x", 4096)+fmt.Sprint(i))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code == http.StatusOK {
			allowed++
		}
	}

	if allowed > limit {
		t.Fatalf("%d of 50 requests were allowed against a limit of %d: a non-address hop still buys a fresh bucket", allowed, limit)
	}
}

// One IPv6 subscriber owns a whole /64 and can source each request from a
// different address in it without asking anyone.
func TestAnIPv6ClientCannotRotateThroughItsOwnSubnet(t *testing.T) {
	t.Parallel()

	const limit = 3
	interceptor := NewRateLimitInterceptor(limit, time.Minute)
	handler := interceptor.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	call := func(remote string) int {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/tasks", nil)
		req.RemoteAddr = remote
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder.Code
	}

	allowed := 0
	for i := range 50 {
		if call(fmt.Sprintf("[2001:db8:1:2::%x]:5555", i+1)) == http.StatusOK {
			allowed++
		}
	}
	if allowed > limit {
		t.Fatalf("%d of 50 requests from one /64 were allowed against a limit of %d", allowed, limit)
	}

	// A neighbouring subscriber is a different customer and keeps its own
	// allowance.
	if code := call("[2001:db8:1:3::1]:5555"); code != http.StatusOK {
		t.Fatalf("a client in a different /64 was refused: %d", code)
	}
}
