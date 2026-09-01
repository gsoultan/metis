// Package metrics emits the data behind the service level objectives in
// .junie/roadmap.md §1 — p95 latency, error budget and availability.
//
// Those targets were declared without anything measuring them, which makes them
// aspirations rather than objectives. The histogram buckets here are chosen to
// straddle the exact thresholds the roadmap names, so "are we inside p95 <
// 150ms" is a question this data can actually answer rather than approximate.
package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// maxTrackedRoutes bounds the route label's cardinality.
//
// The route label is derived from a request path, which is attacker-supplied.
// Without a bound, requests to /api/v1/aaaa, /api/v1/aaab, … would each mint a
// new time series and the process would run out of memory — a metrics endpoint
// is not a reason to hold an unbounded map keyed by remote input.
const maxTrackedRoutes = 200

// routeOverflow is where paths land once maxTrackedRoutes is reached, and where
// path segments that look like identifiers are collapsed to.
const (
	routeOverflow = "other"
	segmentID     = ":id"
)

// Collector holds the registry and the collectors registered on it.
type Collector struct {
	registry *prometheus.Registry

	requestDuration *prometheus.HistogramVec
	requestsTotal   *prometheus.CounterVec
	inFlight        prometheus.Gauge

	// mu guards routes, which bounds label cardinality.
	mu     sync.Mutex
	routes map[string]struct{}
}

// New builds a Collector with its own registry.
//
// It uses a private registry rather than the default one so that a library
// pulling in client_golang cannot silently add series to this service's scrape
// output, and so tests can construct one without global state.
func New() *Collector {
	c := &Collector{
		registry: prometheus.NewRegistry(),
		routes:   make(map[string]struct{}, maxTrackedRoutes),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "metis_http_request_duration_seconds",
			Help: "Request latency. Buckets straddle the roadmap's 150ms read and 500ms action targets.",
			// The two SLO thresholds are bucket boundaries on purpose: a
			// quantile interpolated across a bucket that spans the threshold
			// cannot tell you which side of it you are on.
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.15, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"method", "route", "status"}),
		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "metis_http_requests_total",
			Help: "Requests by outcome. status_class carries the 5xx error budget.",
		}, []string{"method", "route", "status_class"}),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "metis_http_requests_in_flight",
			Help: "Requests currently being served, for saturation against the backpressure limits.",
		}),
	}

	c.registry.MustRegister(
		c.requestDuration,
		c.requestsTotal,
		c.inFlight,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return c
}

// Handler serves the scrape endpoint.
func (c *Collector) Handler() http.Handler {
	return promhttp.HandlerFor(c.registry, promhttp.HandlerOpts{})
}

// Registry exposes the underlying registry so other packages can register their
// own collectors without this one having to know about them.
func (c *Collector) Registry() *prometheus.Registry { return c.registry }

// Wrap records latency and outcome for every request.
//
// It belongs *outside* the rate limit and backpressure interceptors. Requests
// those reject with 429 or 503 still spend a user's error budget, and they are
// the first signal that the service is in trouble — measuring only what got
// through would make an overloaded service look perfectly healthy.
func (c *Collector) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		c.inFlight.Inc()
		defer c.inFlight.Dec()

		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		route := c.trackRoute(normalizeRoute(r.URL.Path))
		status := strconv.Itoa(recorder.status)

		c.requestDuration.WithLabelValues(r.Method, route, status).Observe(time.Since(start).Seconds())
		c.requestsTotal.WithLabelValues(r.Method, route, statusClass(recorder.status)).Inc()
	})
}

// trackRoute returns route if it is already known or there is room for it, and
// routeOverflow once the bound is reached.
func (c *Collector) trackRoute(route string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.routes[route]; ok {
		return route
	}
	if len(c.routes) >= maxTrackedRoutes {
		return routeOverflow
	}
	c.routes[route] = struct{}{}
	return route
}

// normalizeRoute collapses identifier-shaped path segments so that one route
// does not become one time series per record.
func normalizeRoute(path string) string {
	if path == "" || path == "/" {
		return "/"
	}

	segments := strings.Split(strings.TrimSuffix(path, "/"), "/")
	for i, segment := range segments {
		if looksLikeID(segment) {
			segments[i] = segmentID
		}
	}
	return strings.Join(segments, "/")
}

// looksLikeID reports whether a path segment is a record identifier rather than
// a fixed part of the route: a UUID, or anything numeric.
func looksLikeID(segment string) bool {
	if segment == "" {
		return false
	}

	if len(segment) == 36 && strings.Count(segment, "-") == 4 {
		return true
	}

	for _, r := range segment {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// statusClass buckets a status into the family an error budget is measured in.
func statusClass(status int) string {
	switch {
	case status >= 500:
		return "5xx"
	case status >= 400:
		return "4xx"
	case status >= 300:
		return "3xx"
	default:
		return "2xx"
	}
}

// statusRecorder captures the status code, which net/http otherwise does not
// expose after the fact.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.status = status
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		// An implicit 200 from the first Write still has to be recorded.
		r.wroteHeader = true
	}
	return r.ResponseWriter.Write(b)
}

// Flush keeps server-sent events working through the wrapper. Without it the
// SSE endpoint would buffer forever, because statusRecorder would hide the
// underlying http.Flusher.
func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
