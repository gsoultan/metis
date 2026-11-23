package httpclient

import (
	"testing"
	"time"
)

// A call that outlives the job lease taken for it lets another worker claim the
// job and call the partner a second time, so the timeout is held below the
// shortest lease the worker accepts.
func TestTheOutboundTimeoutIsHeldBelowTheJobLease(t *testing.T) {
	for raw, want := range map[string]time.Duration{
		"":    defaultTimeout,
		"10s": 10 * time.Second,
		"2m":  MaxTimeout,
		"10m": MaxTimeout,
		"bad": defaultTimeout,
	} {
		t.Setenv(envTimeout, raw)
		if got := Timeout(); got != want {
			t.Errorf("METIS_HTTP_TIMEOUT=%q: %s, want %s", raw, got, want)
		}
	}
}
