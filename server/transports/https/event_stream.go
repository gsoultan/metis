package https

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
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

// eventStreamWriteTimeout is how long one write to a stream may take.
//
// The server has no write timeout, because a stream is meant to stay open for
// hours. So without one here a client that stopped reading — a suspended
// laptop, a half-open connection the kernel has not given up on — filled its
// socket buffer and then held the write, the goroutine and the stream's slot
// for as long as the connection lived. A variable only so a test can wait
// milliseconds for it.
var eventStreamWriteTimeout = 10 * time.Second

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
	// Shutdown does not cancel a request's context: it waits for handlers to
	// return. A stream never returns on its own, so every open tab used to hold
	// shutdown for its whole timeout. Subscribed before the headers go out, so
	// a client that has seen the stream open can rely on shutdown ending it.
	shuttingDown := serverShutdown(r)

	stream := http.NewResponseController(w)
	_, canFlush := w.(http.Flusher)
	if canFlush {
		w.WriteHeader(http.StatusOK)
		if err := stream.Flush(); err != nil {
			log.Debug().Err(err).Msg("An event stream client went away before the headers were sent")
			return
		}
	}

	heartbeat := time.NewTicker(eventStreamHeartbeat)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		var payload string
		select {
		case <-ctx.Done():
			return
		case <-shuttingDown:
			return
		case msg := <-ch:
			payload = msg
		case <-heartbeat.C:
			payload = ": keep-alive\n\n"
		}
		// A failed write means the client is gone. Carrying on would spin this
		// goroutine against a dead connection for as long as events keep
		// arriving — one leaked per disconnect.
		if err := writeEvent(w, stream, payload, canFlush); err != nil {
			log.Debug().Err(err).Msg("An event stream client went away mid-write")
			return
		}
	}
}

// writeEvent writes one payload under its own deadline.
//
// The deadline is set before the write and cleared after it, so a quiet stream
// is not cut for being quiet — only a write that cannot complete is. A writer
// that cannot take a deadline (a test recorder) is written to without one.
func writeEvent(w http.ResponseWriter, stream *http.ResponseController, payload string, canFlush bool) error {
	if err := stream.SetWriteDeadline(time.Now().Add(eventStreamWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	if _, err := fmt.Fprint(w, payload); err != nil {
		return err
	}
	if canFlush {
		if err := stream.Flush(); err != nil {
			return err
		}
	}
	if err := stream.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

// shutdowns holds, per server, a channel closed when that server begins to
// shut down. The main listener and every environment listener serve streams
// from the same handler, so the signal has to be the serving server's own.
var shutdowns sync.Map // *http.Server -> chan struct{}

// serverShutdown returns a channel that closes when the server serving r
// starts shutting down, or nil — never ready — when r was not served by an
// http.Server.
//
// Registered once per server rather than once per stream: RegisterOnShutdown
// only ever appends, so a hook per stream would grow without bound on a server
// that stays up for weeks.
func serverShutdown(r *http.Request) <-chan struct{} {
	server, ok := r.Context().Value(http.ServerContextKey).(*http.Server)
	if !ok || server == nil {
		return nil
	}
	done := make(chan struct{})
	existing, loaded := shutdowns.LoadOrStore(server, done)
	if !loaded {
		server.RegisterOnShutdown(func() {
			shutdowns.Delete(server)
			close(done)
		})
	}
	ch, ok := existing.(chan struct{})
	if !ok {
		return nil
	}
	return ch
}
