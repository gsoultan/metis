package https

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/impl"
)

// streamUnderTest serves the event stream for one organization and reports
// when the handler returns.
func streamUnderTest(observer *impl.SSEObserver, organization uuid.UUID) (http.Handler, <-chan struct{}) {
	returned := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(returned)
		ctx := entities.WithTenantContext(r.Context(), entities.TenantContext{TenantID: organization.String()})
		serveEventStream(w, r.WithContext(ctx), observer)
	})
	return handler, returned
}

// A client that stops reading must not hold its stream open forever.
//
// The server has no write timeout, so once the socket buffers filled a write
// to a client that had stopped reading blocked for as long as the connection
// lived, pinning the goroutine and the stream's slot with it.
func TestAStreamToAClientThatNeverReadsIsCutOff(t *testing.T) {
	previous := eventStreamWriteTimeout
	eventStreamWriteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { eventStreamWriteTimeout = previous })
	observer := impl.NewSSEObserver()
	organization := uuid.New()
	handler, returned := streamUnderTest(observer, organization)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	var dialer net.Dialer
	conn, err := dialer.DialContext(t.Context(), "tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetReadBuffer(4096)
	}
	if _, err := conn.Write([]byte("GET " + EventStreamPath + " HTTP/1.1\r\nHost: metis\r\n\r\n")); err != nil {
		t.Fatalf("write request: %v", err)
	}
	// From here on nothing is ever read.

	payload := strings.Repeat("x", 64<<10)
	scope := entities.SSEScope{Organization: organization}
	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-returned:
			return
		case <-deadline:
			t.Fatal("the stream was still blocked writing to a client that never reads after 10s")
		case <-time.After(5 * time.Millisecond):
			observer.DeliverFromPeer(scope, payload)
		}
	}
}

// Shutdown waits for handlers to return and does not cancel their contexts,
// so an open stream used to hold every shutdown for its whole timeout.
func TestShutdownClosesOpenStreams(t *testing.T) {
	observer := impl.NewSSEObserver()
	handler, returned := streamUnderTest(observer, uuid.New())

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+listener.Addr().String()+EventStreamPath, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	// Keep reading, so the stream is a healthy one and only shutdown ends it.
	go func() {
		reader := bufio.NewReader(response.Body)
		for {
			if _, err := reader.ReadString('\n'); err != nil {
				return
			}
		}
	}()

	shutdownCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown waited out its timeout on an open stream: %v", err)
	}
	select {
	case <-returned:
	default:
		t.Fatal("shutdown finished while the stream handler was still running")
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("serve: %v", err)
	}
}
