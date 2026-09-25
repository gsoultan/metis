package https

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/endpoints/principal"
	"github.com/gsoultan/metis/server/interceptors/tenant"
)

// EventStreamPath is where the live event stream is served. The API's
// backpressure limiter lets it through uncounted, because it is limited here
// instead; see eventStreamLimit.
const EventStreamPath = "/api/v1/events"

// eventStreamHeartbeat is how often a quiet stream says it is still there.
//
// A comment line, which every reader ignores. Without it a stream on a quiet
// installation carries nothing for minutes: a proxy with an idle timeout cuts
// it, and a client that has gone away is only noticed when an event finally
// fails to write — its goroutine and its slot held until then. A variable
// only so a test can wait milliseconds for one rather than half a minute.
var eventStreamHeartbeat = 25 * time.Second

// eventStreamRetryAfter is what a refused stream is told to wait, in seconds.
const eventStreamRetryAfter = 5

func eventStreamHandler(sseObserver *impl.SSEObserver, limit *eventStreamLimit) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		account, err := principal.Username(r.Context())
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		switch limit.acquire(account) {
		case streamServerFull:
			w.Header().Set("Retry-After", strconv.Itoa(eventStreamRetryAfter))
			http.Error(w, "Too many live streams are open; retry later", http.StatusServiceUnavailable)
			return
		case streamAccountFull:
			w.Header().Set("Retry-After", strconv.Itoa(eventStreamRetryAfter))
			http.Error(w, "This account has too many live streams open; close a tab", http.StatusTooManyRequests)
			return
		}
		defer limit.release(account)

		serveEventStream(w, r, sseObserver)
	}
}

func serveEventStream(w http.ResponseWriter, r *http.Request, sseObserver *impl.SSEObserver) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	if origin := corsOrigin(parseCORSOrigins(envvar.Get(envCORSOrigins)), r.Header.Get("Origin")); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}

	// The scope is taken from the request, never from what the client asked
	// for: the organization comes from the token the auth middleware
	// validated, and the environment from the listener this connection arrived
	// on. A client that could name its own audience would name somebody
	// else's. The tenant is resolved here rather than read off the context:
	// the endpoint chain that normally resolves it does not run for this
	// handler, which is mounted straight on the mux so the connection can stay
	// open.
	streamCtx := r.Context()
	if tc, ok := tenant.ResolveFromContext(streamCtx); ok {
		streamCtx = entities.WithTenantContext(streamCtx, tc)
	}
	ch := sseObserver.AddClient(entities.SSEScopeFrom(streamCtx))
	defer sseObserver.RemoveClient(ch)

	// Send the headers now rather than with the first event.
	//
	// Without this the response is buffered until something happens, so
	// EventSource stays in CONNECTING on a quiet installation: the browser
	// cannot tell a working stream from a broken one, onopen never fires, and
	// any proxy with a header-read timeout closes the connection before the
	// first event ever arrives.
	flusher, canFlush := w.(http.Flusher)
	if canFlush {
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
	}

	heartbeat := time.NewTicker(eventStreamHeartbeat)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		var payload string
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			payload = msg
		case <-heartbeat.C:
			payload = ": keep-alive\n\n"
		}
		// A failed write means the client is gone. Carrying on would spin this
		// goroutine against a dead connection for as long as events keep
		// arriving — one leaked per disconnect.
		if _, err := fmt.Fprint(w, payload); err != nil {
			log.Debug().Err(err).Msg("An event stream client went away mid-write")
			return
		}
		if canFlush {
			flusher.Flush()
		}
	}
}
