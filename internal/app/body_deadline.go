package app

import (
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// defaultHTTPBodyReadTimeout is how long a request may take to send its body.
// Bodies are capped at defaultHTTPMaxBodyBytes, which this allows at about
// 70 KB/s.
const defaultHTTPBodyReadTimeout = 30 * time.Second

// withBodyReadDeadline bounds how long a request may take to send its body.
//
// The server sets ReadHeaderTimeout and nothing after it, because a read
// deadline for the whole request would also end the event stream, which is
// held open for as long as a tab is. So a request that announced a body and
// sent a byte every few seconds held its backpressure slot for as long as it
// liked: a few hundred of them, from one address and well inside its rate
// limit, answered every other call with 503 while /readyz stayed green.
//
// The deadline is set only for a request with a body, and cleared as soon as
// the body is read to its end or the handler returns, so a slow handler and a
// long-lived stream are unaffected.
func withBodyReadDeadline(timeout time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Body == http.NoBody || r.ContentLength == 0 {
			next.ServeHTTP(w, r)
			return
		}
		rc := http.NewResponseController(w)
		if err := rc.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			// A connection that cannot take a deadline is served as before.
			next.ServeHTTP(w, r)
			return
		}
		body := &deadlineBody{ReadCloser: r.Body, clear: func() {
			if err := rc.SetReadDeadline(time.Time{}); err != nil {
				log.Debug().Err(err).Msg("Could not clear a request's body read deadline")
			}
		}}
		defer body.done()
		r.Body = body
		next.ServeHTTP(w, r)
	})
}

// deadlineBody clears the read deadline once the body is finished with.
type deadlineBody struct {
	io.ReadCloser
	once  sync.Once
	clear func()
}

func (b *deadlineBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if errors.Is(err, io.EOF) {
		b.done()
	}
	return n, err
}

func (b *deadlineBody) Close() error {
	b.done()
	return b.ReadCloser.Close()
}

func (b *deadlineBody) done() { b.once.Do(b.clear) }
