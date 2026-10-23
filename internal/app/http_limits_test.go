package app

import "testing"

func TestResolveHTTPLimits(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		rate, inFlight, queued     string
		wantRate, wantIn, wantQued int
	}{
		{"unset keeps the defaults", "", "", "",
			defaultHTTPMaxRequestsPerLimit, defaultHTTPMaxInFlightRequests, defaultHTTPMaxQueuedRequests},
		{"raised", "6000", "256", "1024", 6000, 256, 1024},
		{"surrounding space is ignored", " 600 ", "", "", 600, defaultHTTPMaxInFlightRequests, defaultHTTPMaxQueuedRequests},
		// Zero would refuse every call; it falls back rather than locking the API.
		{"zero falls back", "0", "0", "0",
			defaultHTTPMaxRequestsPerLimit, defaultHTTPMaxInFlightRequests, defaultHTTPMaxQueuedRequests},
		{"negative and junk fall back", "-5", "lots", "1e3",
			defaultHTTPMaxRequestsPerLimit, defaultHTTPMaxInFlightRequests, defaultHTTPMaxQueuedRequests},
		{"past the ceiling falls back", "100001", "", "", defaultHTTPMaxRequestsPerLimit,
			defaultHTTPMaxInFlightRequests, defaultHTTPMaxQueuedRequests},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envHTTPRateLimit, tc.rate)
			t.Setenv(envHTTPMaxInFlight, tc.inFlight)
			t.Setenv(envHTTPMaxQueued, tc.queued)

			got := resolveHTTPLimits()
			want := httpLimits{requestsPerMinute: tc.wantRate, maxInFlight: tc.wantIn, maxQueued: tc.wantQued}
			if got != want {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}
