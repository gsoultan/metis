// Package health serves the liveness and readiness probes an orchestrator uses
// to decide whether this process should keep running and whether it should
// receive traffic.
//
// The two answer different questions and must not be conflated:
//
//   - Liveness asks "is this process broken beyond recovery?" It checks nothing
//     external. If it consulted the database, one database blip would fail every
//     replica's probe at once and the orchestrator would restart the entire
//     fleet — turning a recoverable dependency outage into a total one.
//   - Readiness asks "can this process serve a request right now?" It does check
//     the database, so a replica that cannot reach it is taken out of the load
//     balancer rather than serving errors.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// LivenessPath and ReadinessPath are the routes this package serves.
const (
	LivenessPath  = "/healthz"
	ReadinessPath = "/readyz"
)

// checkTimeout bounds a readiness check. Without it a hung database would hang
// the probe instead of failing it, and the orchestrator would read "no answer"
// as "still starting" for as long as the hang lasted.
const checkTimeout = 2 * time.Second

// Checker reports whether one dependency can serve requests. It is deliberately
// a single method so a caller can supply a closure and nothing has to implement
// an interface it does not need.
type Checker interface {
	Check(ctx context.Context) error
}

// CheckerFunc adapts a plain function to Checker.
type CheckerFunc func(ctx context.Context) error

// Check implements Checker.
func (f CheckerFunc) Check(ctx context.Context) error { return f(ctx) }

// response is the probe body. Probes are read by machines, but a human debugging
// a failing deployment reads them too, so the failure names the dependency.
type response struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitzero"`
}

// Wrap serves the probe endpoints ahead of next.
//
// It wraps the *outermost* layer on purpose. Health checks must not pass through
// authentication (an orchestrator has no credentials), rate limiting, or
// backpressure. A probe shed by the backpressure limiter reads as an unhealthy
// process, so a service that was merely busy would be restarted or pulled out of
// rotation at exactly the moment it was recovering.
func Wrap(next http.Handler, readiness map[string]Checker) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case LivenessPath:
			writeResponse(w, http.StatusOK, response{Status: "ok"})
		case ReadinessPath:
			serveReadiness(w, r, readiness)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// serveReadiness runs every dependency check and fails on the first that cannot
// answer.
func serveReadiness(w http.ResponseWriter, r *http.Request, readiness map[string]Checker) {
	ctx, cancel := context.WithTimeout(r.Context(), checkTimeout)
	defer cancel()

	for name, checker := range readiness {
		if err := checker.Check(ctx); err != nil {
			// The dependency's name is safe to report; its error is not, since
			// a driver error can carry a DSN. The name alone is enough to know
			// which dependency to go and look at.
			writeResponse(w, http.StatusServiceUnavailable, response{
				Status: "unavailable",
				Detail: name + " is not reachable",
			})
			return
		}
	}
	writeResponse(w, http.StatusOK, response{Status: "ok"})
}

func writeResponse(w http.ResponseWriter, status int, body response) {
	w.Header().Set("Content-Type", "application/json")
	// A cached probe answer is a stale probe answer.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
