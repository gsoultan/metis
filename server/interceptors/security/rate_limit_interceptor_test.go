package security

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimitInterceptor(t *testing.T) {
	t.Parallel()

	type requestSpec struct {
		advance       time.Duration
		xForwardedFor string
		remoteAddr    string
	}

	testCases := []struct {
		name             string
		maxRequests      int
		window           time.Duration
		requests         []requestSpec
		expectedStatuses []int
	}{
		{
			name:        "blocks after reaching limit for same client",
			maxRequests: 2,
			window:      time.Minute,
			requests: []requestSpec{
				{remoteAddr: "10.0.0.1:1234"},
				{remoteAddr: "10.0.0.1:1234"},
				{remoteAddr: "10.0.0.1:1234"},
			},
			expectedStatuses: []int{http.StatusOK, http.StatusOK, http.StatusTooManyRequests},
		},
		{
			name:        "resets allowance after window",
			maxRequests: 1,
			window:      time.Minute,
			requests: []requestSpec{
				{remoteAddr: "10.0.0.1:1234"},
				{advance: 10 * time.Second, remoteAddr: "10.0.0.1:1234"},
				{advance: time.Minute, remoteAddr: "10.0.0.1:1234"},
			},
			expectedStatuses: []int{http.StatusOK, http.StatusTooManyRequests, http.StatusOK},
		},
		{
			name:        "uses x-forwarded-for to separate clients",
			maxRequests: 1,
			window:      time.Minute,
			requests: []requestSpec{
				{xForwardedFor: "192.168.1.10", remoteAddr: "10.0.0.1:1234"},
				{xForwardedFor: "192.168.1.11", remoteAddr: "10.0.0.1:1234"},
			},
			expectedStatuses: []int{http.StatusOK, http.StatusOK},
		},
	}

	for _, testCase := range testCases {

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
			interceptor := NewRateLimitInterceptor(testCase.maxRequests, testCase.window).(*rateLimitInterceptor)
			interceptor.now = func() time.Time {
				return now
			}

			handler := interceptor.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			for requestIndex, request := range testCase.requests {
				now = now.Add(request.advance)

				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/tasks", nil)
				req.RemoteAddr = request.remoteAddr
				if request.xForwardedFor != "" {
					req.Header.Set("X-Forwarded-For", request.xForwardedFor)
				}

				res := httptest.NewRecorder()
				handler.ServeHTTP(res, req)

				expectedStatus := testCase.expectedStatuses[requestIndex]
				if res.Code != expectedStatus {
					t.Fatalf("request %d expected status %d, got %d", requestIndex, expectedStatus, res.Code)
				}

				if expectedStatus == http.StatusTooManyRequests && res.Header().Get("Retry-After") == "" {
					t.Fatalf("request %d expected Retry-After header to be set", requestIndex)
				}
			}
		})
	}
}

func TestRateLimitInterceptorReusesClientWindowEntry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	interceptor := NewRateLimitInterceptor(3, time.Minute).(*rateLimitInterceptor)
	interceptor.now = func() time.Time {
		return now
	}

	const clientKey = "10.0.0.1"
	if !interceptor.allow(clientKey) {
		t.Fatalf("first request should be allowed")
	}

	firstWindow := interceptor.windows[clientKey]
	if firstWindow == nil {
		t.Fatalf("expected window entry to be created")
	}

	if !interceptor.allow(clientKey) {
		t.Fatalf("second request should be allowed")
	}

	secondWindow := interceptor.windows[clientKey]
	if firstWindow != secondWindow {
		t.Fatalf("expected same window entry pointer to be reused")
	}
}

// TestClientKeyFromRequest pins which address a request is charged to.
//
// The old expectation here was "uses first forwarded client" — the leftmost
// X-Forwarded-For entry. That is the one value in the chain a client can write
// itself, because proxies append. Asserting it meant the test agreed with the
// bypass: an attacker varying the header got a fresh bucket per request.
//
// The rule now is rightmost-untrusted, and only when the request actually came
// from a proxy we trust.
func TestClientKeyFromRequest(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		remote    string
		forwarded string
		expected  string
	}{
		{
			// The proxy appended the address it saw, on the right. Anything to
			// the left of it came from the client.
			name:      "takes the address the trusted proxy actually saw",
			remote:    "10.0.0.1:1234",
			forwarded: "203.0.113.10, 198.51.100.20",
			expected:  "198.51.100.20",
		},
		{
			// With no explicit proxy list, the rightmost entry is the client —
			// including when it is a private address, which on an internal
			// deployment is exactly what a real client looks like.
			name:      "charges the rightmost hop when no proxy chain is configured",
			remote:    "10.0.0.1:1234",
			forwarded: "203.0.113.10, 192.168.1.10",
			expected:  "192.168.1.10",
		},
		{
			// The header is a claim from a stranger. A request straight off the
			// internet does not get to name its own client.
			name:      "ignores the header from an untrusted peer",
			remote:    "203.0.113.9:5555",
			forwarded: "10.0.0.99",
			expected:  "203.0.113.9",
		},
		{
			name:     "falls back to remote host",
			remote:   "10.0.0.1:1234",
			expected: "10.0.0.1",
		},
		{
			name:     "extracts host from ipv6 remote addr",
			remote:   "[2001:db8::1]:8443",
			expected: "2001:db8::1",
		},
		{
			name:     "falls back to remote addr when host port is missing",
			remote:   "10.0.0.1",
			expected: "10.0.0.1",
		},
		{
			name:     "falls back to raw ipv6 address when host port is missing",
			remote:   "2001:db8::1",
			expected: "2001:db8::1",
		},
		{
			name:     "returns unknown for empty address",
			expected: "unknown",
		},
	}

	for _, testCase := range testCases {

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			req.RemoteAddr = testCase.remote
			if testCase.forwarded != "" {
				req.Header.Set("X-Forwarded-For", testCase.forwarded)
			}

			interceptor := &rateLimitInterceptor{trusted: loadTrustedProxies()}
			if got := interceptor.clientKey(req); got != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, got)
			}
		})
	}
}

// A rate limit an attacker can opt out of is not a rate limit.
//
// X-Forwarded-For was read and believed with no check on who sent it. Because
// the header picks the bucket, sending a different value each time bought a
// fresh allowance each time: measured against the code before this fix, one
// address got 30 requests through a limit of 3, and only because the test
// stopped at 50 — the real bound is however long the attacker keeps going.
func TestSpoofedForwardedForCannotBuyAFreshBucket(t *testing.T) {
	t.Parallel()

	const limit = 3
	interceptor := NewRateLimitInterceptor(limit, time.Minute)
	handler := interceptor.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	allowed := 0
	for i := range 50 {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/tasks", nil)
		// One attacker, one address, a new forged client on every request.
		req.RemoteAddr = "203.0.113.9:5555"
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.0.0.%d", i))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code == http.StatusOK {
			allowed++
		}
	}

	if allowed > limit {
		t.Fatalf("%d of 50 requests were allowed against a limit of %d: varying X-Forwarded-For still buys a fresh bucket", allowed, limit)
	}
}

// The fix must not break the deployment shape it exists to support: behind a
// load balancer, two real clients must still get their own allowances.
func TestClientsBehindATrustedProxyKeepSeparateBuckets(t *testing.T) {
	t.Parallel()

	const limit = 2
	interceptor := NewRateLimitInterceptor(limit, time.Minute)
	handler := interceptor.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	call := func(client string) int {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/tasks", nil)
		req.RemoteAddr = "10.0.0.1:443" // the load balancer
		req.Header.Set("X-Forwarded-For", client)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder.Code
	}

	// One client exhausts its own allowance.
	for range limit {
		if code := call("198.51.100.20"); code != http.StatusOK {
			t.Fatalf("a client was refused inside its own limit: %d", code)
		}
	}
	if code := call("198.51.100.20"); code != http.StatusTooManyRequests {
		t.Fatalf("the limit did not apply to a client behind the proxy: %d", code)
	}

	// A different client is unaffected: they do not share a bucket just
	// because they share a load balancer.
	if code := call("198.51.100.77"); code != http.StatusOK {
		t.Fatalf("a second client was refused because the first exhausted its limit: %d", code)
	}
}

// A multi-hop proxy chain has to be described, because it cannot be guessed.
//
// The default deliberately skips nothing while walking the header: on an
// internal deployment every real client is on RFC1918, so treating private
// space as infrastructure would walk straight past the clients and charge them
// all to the load balancer — one shared allowance for the whole company. An
// operator who genuinely has two proxies names them.
func TestAConfiguredProxyChainIsWalkedPast(t *testing.T) {
	t.Setenv("METIS_TRUSTED_PROXIES", "10.0.0.0/8")

	cases := []struct {
		name      string
		remote    string
		forwarded string
		expected  string
	}{
		{
			name:      "walks past our own proxies to the client",
			remote:    "10.0.0.1:1234",
			forwarded: "203.0.113.10, 198.51.100.20, 10.0.0.7",
			expected:  "198.51.100.20",
		},
		{
			name:      "falls back to the peer when every hop is our own",
			remote:    "10.0.0.1:1234",
			forwarded: "10.0.0.7, 10.0.0.8",
			expected:  "10.0.0.1",
		},
		{
			// Outside the configured range the header is not believed at all.
			name:      "ignores the header from a peer outside the configured range",
			remote:    "192.168.1.5:1234",
			forwarded: "198.51.100.20",
			expected:  "192.168.1.5",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			req.RemoteAddr = testCase.remote
			req.Header.Set("X-Forwarded-For", testCase.forwarded)

			interceptor := &rateLimitInterceptor{trusted: loadTrustedProxies()}
			if got := interceptor.clientKey(req); got != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, got)
			}
		})
	}
}

// "none" is for a server exposed directly, where no proxy should ever be
// believed and the header is only ever a client's claim about itself.
func TestTrustingNoProxyIgnoresTheHeaderEntirely(t *testing.T) {
	t.Setenv("METIS_TRUSTED_PROXIES", "none")

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.20")

	interceptor := &rateLimitInterceptor{trusted: loadTrustedProxies()}
	if got := interceptor.clientKey(req); got != "10.0.0.1" {
		t.Fatalf("expected the peer %q, got %q: the header was believed with trust disabled", "10.0.0.1", got)
	}
}
