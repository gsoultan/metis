package metrics

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNormalizeRoute(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"root", "/", "/"},
		{"static route", "/api/v1/tasks", "/api/v1/tasks"},
		{"uuid segment", "/api/v1/tasks/0198c7d2-6d8f-7c3a-9f21-2b7c9d4e5a60", "/api/v1/tasks/:id"},
		{"numeric segment", "/api/v1/tasks/42", "/api/v1/tasks/:id"},
		{"two identifiers", "/api/v1/projects/0198c7d2-6d8f-7c3a-9f21-2b7c9d4e5a60/tasks/7", "/api/v1/projects/:id/tasks/:id"},
		{"trailing slash", "/api/v1/tasks/", "/api/v1/tasks"},
		{"sub-resource after id", "/api/v1/tasks/42/claim", "/api/v1/tasks/:id/claim"},
		{"word is not an id", "/api/v1/setup/status", "/api/v1/setup/status"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeRoute(tc.path); got != tc.want {
				t.Errorf("normalizeRoute(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestStatusClass(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{200, "2xx"}, {204, "2xx"}, {301, "3xx"},
		{400, "4xx"}, {429, "4xx"}, {500, "5xx"}, {503, "5xx"},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			if got := statusClass(tc.status); got != tc.want {
				t.Errorf("statusClass(%d) = %q, want %q", tc.status, got, tc.want)
			}
		})
	}
}

// TestRouteLabelIsBounded is the important one: the route label comes from an
// attacker-supplied path, so distinct junk paths must stop minting time series.
func TestRouteLabelIsBounded(t *testing.T) {
	c := New()
	handler := c.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	// Far more distinct paths than the bound allows, none identifier-shaped so
	// normalization cannot collapse them.
	for i := range maxTrackedRoutes * 3 {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("/api/v1/junk-%s", strings.Repeat("a", i%50)+fmt.Sprint(i)), nil)
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	if got := testutil.CollectAndCount(c.requestsTotal); got > maxTrackedRoutes+1 {
		t.Fatalf("route label produced %d series, want at most %d — the bound is not holding",
			got, maxTrackedRoutes+1)
	}

	body := scrape(t, c)
	if !strings.Contains(body, routeOverflow) {
		t.Error("paths past the bound should collapse into the overflow bucket")
	}
}

// TestWrapRecordsOutcome checks that a rejected request is measured too — those
// are the ones that spend the error budget.
func TestWrapRecordsOutcome(t *testing.T) {
	c := New()
	handler := c.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/tasks/42/claim", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	body := scrape(t, c)
	for _, want := range []string{
		`metis_http_requests_total{method="POST",route="/api/v1/tasks/:id/claim",status_class="5xx"} 1`,
		`route="/api/v1/tasks/:id/claim"`,
		`metis_http_request_duration_seconds_bucket`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape output missing %q\n---\n%s", want, body)
		}
	}
}

// TestSLOThresholdsAreBucketBoundaries pins the buckets to the roadmap's stated
// targets. A quantile interpolated across a bucket spanning the threshold
// cannot say which side of it you are on, so these boundaries are load-bearing.
func TestSLOThresholdsAreBucketBoundaries(t *testing.T) {
	c := New()
	handler := c.Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/tasks", nil))

	body := scrape(t, c)
	for _, threshold := range []string{`le="0.15"`, `le="0.5"`} {
		if !strings.Contains(body, threshold) {
			t.Errorf("no histogram bucket at %s — the matching SLO target is not measurable", threshold)
		}
	}
}

// TestStatusRecorderPreservesFlush guards server-sent events: wrapping the
// ResponseWriter must not hide http.Flusher, or the event stream buffers.
func TestStatusRecorderPreservesFlush(t *testing.T) {
	rec := httptest.NewRecorder()
	var recorder http.ResponseWriter = &statusRecorder{ResponseWriter: rec, status: http.StatusOK}

	flusher, ok := recorder.(http.Flusher)
	if !ok {
		t.Fatal("statusRecorder does not implement http.Flusher — SSE would buffer forever")
	}
	flusher.Flush()
	if !rec.Flushed {
		t.Error("Flush did not reach the underlying ResponseWriter")
	}
}

// TestImplicitStatusIsRecorded covers a handler that writes a body without ever
// calling WriteHeader.
func TestImplicitStatusIsRecorded(t *testing.T) {
	c := New()
	handler := c.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/tasks", nil))

	if body := scrape(t, c); !strings.Contains(body, `status_class="2xx"`) {
		t.Errorf("an implicit 200 was not recorded as 2xx\n---\n%s", body)
	}
}

func scrape(t *testing.T, c *Collector) string {
	t.Helper()
	rec := httptest.NewRecorder()
	c.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("scrape returned %d", rec.Code)
	}
	return rec.Body.String()
}
