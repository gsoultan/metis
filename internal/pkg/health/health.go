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

	"github.com/rs/zerolog/log"
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

	// Version is the build serving this probe. "Which version is actually
	// running" is the first question of any incident and the last question of
	// any deploy, and until this existed the only way to answer it was to read
	// a startup log that may have rotated away. Reported on liveness only:
	// readiness is polled every few seconds by every replica, and it is the
	// answer to a different question.
	Version string `json:"version,omitzero"`
}

// buildVersion is stamped at link time; see internal/app.version, which is set
// by the same -X flag. It is duplicated rather than imported because internal/app
// imports this package, and the reverse would be a cycle.
//
// "dev" rather than a plausible-looking number: a probe reporting a version that
// was never released is worse than one admitting it does not know.
var buildVersion = "dev"

// SetBuildVersion records the running build for the liveness probe. The
// composition root calls it once at startup, before any server is listening.
func SetBuildVersion(v string) {
	if v != "" {
		buildVersion = v
	}
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
			writeResponse(w, http.StatusOK, response{Status: "ok", Version: buildVersion})
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

	// The status line is already sent, so a failed encode cannot change the
	// answer the orchestrator acts on — but it means a probe body went out
	// truncated, and silently swallowing that would leave a confusing readiness
	// failure with no explanation anywhere.
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Warn().Err(err).Msg("Could not write the health probe response body")
	}
}
