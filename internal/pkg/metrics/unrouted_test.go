package metrics

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Junk paths must not take the route slots from the real ones.
//
// The slots are claimed first come, first served and never released, so an
// unauthenticated scanner's first 200 distinct paths — every one a 404 or a
// 401 — used to take them all, and every real route was reported as "other"
// from then on.
func TestUnroutedJunkCannotCrowdOutRealRoutes(t *testing.T) {
	c := New()
	handler := c.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/tasks":
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/api/v1/probe-"):
			w.WriteHeader(http.StatusUnauthorized)
		case strings.HasPrefix(r.URL.Path, "/api/v1/flood-"):
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	call := func(path string) {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	}

	for i := range maxTrackedRoutes * 2 {
		call(fmt.Sprintf("/wp-admin/junk-%c%d", 'a'+i%26, i))
		call(fmt.Sprintf("/api/v1/probe-%c%d", 'a'+i%26, i))
		call(fmt.Sprintf("/api/v1/flood-%c%d", 'a'+i%26, i))
	}
	call("/api/v1/tasks")

	body := scrape(t, c)
	if !strings.Contains(body, `route="/api/v1/tasks"`) {
		t.Fatalf("a real route was crowded out by unrouted junk:\n%s", body)
	}
	if strings.Contains(body, "junk-") || strings.Contains(body, "probe-") || strings.Contains(body, "flood-") {
		t.Fatal("an unrouted path claimed a route slot")
	}
	if !strings.Contains(body, `route="`+routeUnmatched+`"`) {
		t.Fatal("unrouted requests should still be counted, under the unmatched route")
	}
}

// A route already known keeps its own name when it fails with one of those
// statuses: a 401 on a real endpoint is that endpoint's outcome.
func TestAKnownRouteKeepsItsNameWhenRefused(t *testing.T) {
	c := New()
	status := http.StatusOK
	handler := c.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	call := func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/tasks", nil))
	}

	call()
	status = http.StatusUnauthorized
	call()

	body := scrape(t, c)
	if !strings.Contains(body, `metis_http_requests_total{method="GET",route="/api/v1/tasks",status_class="4xx"} 1`) {
		t.Fatalf("a refusal on a known route was not recorded under it:\n%s", body)
	}
}
