package app

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// A request in flight when shutdown starts is answered before the server's
// goroutine returns.
//
// ListenAndServe returns the moment Shutdown begins, and the goroutine used to
// return with it: the errgroup finished, background work was stopped and the
// process could exit while the request it had promised drain time to was still
// being answered.
func TestARequestInFlightAtShutdownIsAnsweredBeforeServingReturns(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, "done")
	})

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := newHTTPServer(listener.Addr().String(), handler)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	returned := make(chan error, 1)
	go func() {
		returned <- serveUntilShutdown(ctx, "test server", server, func() error { return server.Serve(listener) })
	}()

	type answer struct {
		body string
		err  error
	}
	answered := make(chan answer, 1)
	go func() {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+listener.Addr().String()+"/", nil)
		if err != nil {
			answered <- answer{err: err}
			return
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			answered <- answer{err: err}
			return
		}
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		answered <- answer{body: string(body), err: err}
	}()

	<-entered
	cancel()

	select {
	case err := <-returned:
		t.Fatalf("serving returned (%v) while a request was still being answered", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	got := <-answered
	if got.err != nil || got.body != "done" {
		t.Fatalf("the in-flight request was not answered: body=%q err=%v", got.body, got.err)
	}
	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("a clean shutdown was reported as a failure: %v", err)
		}
	case <-time.After(httpShutdownTimeout):
		t.Fatal("serving did not return after the in-flight request was answered")
	}
}
