package security

import (
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

func TestClientKeyFromRequest(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		remote    string
		forwarded string
		expected  string
	}{
		{
			name:      "uses first forwarded client",
			remote:    "10.0.0.1:1234",
			forwarded: "203.0.113.10, 198.51.100.20",
			expected:  "203.0.113.10",
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

			if got := clientKeyFromRequest(req); got != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, got)
			}
		})
	}
}
