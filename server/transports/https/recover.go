package https

import (
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/gsoultan/metis/internal/pkg/redaction"
	"github.com/rs/zerolog/log"
)

// responseStarted is satisfied by a ResponseWriter that knows whether the
// status line has already gone out.
//
// The metrics collector's recorder tracks this for its own reasons and is
// always directly outside RecoverPanics, so the answer is available without
// this middleware allocating a wrapper of its own on every request.
type responseStarted interface {
	ResponseStarted() bool
}

// RecoverPanics turns a panic in the request path into a 500.
//
// Without it a panicking handler is not an error, it is a *disappearance*.
// net/http recovers per connection and closes it, so the caller gets EOF rather
// than a status — and because the metrics middleware records after
// ServeHTTP returns, the request is never counted at all. It does not appear in
// metis_http_requests_total, it does not spend the 5xx error budget, and
// MetisErrorBudgetBurning cannot fire for it. The outage is invisible to the
// alerting built to catch exactly this.
//
// That is not hypothetical: GET /api/v1/tasks panicked on a task row whose
// priority column was NULL, and the load test saw `EOF` where it expected a
// page of results. The column is fixed; this is the reason the next one is a
// 500 somebody is paged for rather than a connection reset nobody counts.
//
// It must be wrapped *inside* the metrics collector so the 500 it writes is the
// status the collector records.
func RecoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}

			// The documented way for a handler to abandon a response without
			// being reported. Recovering it would break the contract net/http
			// and ReverseProxy rely on.
			if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(recovered)
			}

			// Logged with the stack, because a panic with no stack is a panic
			// nobody can fix. The panic value is redacted on the way in: a
			// runtime error carries no data, but a panic raised over a row or a
			// connection string carries whatever was in it, and this line ends
			// up in a log aggregator. r.URL.Path rather than RequestURI, for
			// the same reason — a query string can hold a token.
			log.Error().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Str("panic", redaction.RedactText(fmt.Sprint(recovered))).
				Bytes("stack", debug.Stack()).
				Msg("Panic serving a request; returning 500")

			// A response already in flight cannot be replaced, only corrupted:
			// the status is sent, and an SSE stream would get a JSON object
			// appended to it. Leave it truncated and let the client's read fail.
			if started, ok := w.(responseStarted); ok && started.ResponseStarted() {
				return
			}

			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			// A fixed body. Everything specific is in the log above, where it
			// reaches an operator rather than whoever sent the request.
			if _, err := w.Write([]byte(`{"error":"internal server error"}`)); err != nil {
				log.Debug().Err(err).Msg("Could not write the 500 after a panic")
			}
		}()

		next.ServeHTTP(w, r)
	})
}
