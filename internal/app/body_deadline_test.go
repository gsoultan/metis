package app

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestASlowBodyIsCutOff: the server bounded only the headers, so a request that
// announced a body and trickled it held its slot for as long as it liked.
func TestASlowBodyIsCutOff(t *testing.T) {
	readErr := make(chan error, 1)
	srv := httptest.NewServer(withBodyReadDeadline(200*time.Millisecond, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		readErr <- err
	})))
	t.Cleanup(srv.Close)

	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	// One byte of a promised hundred, then nothing.
	if _, err := fmt.Fprint(conn, "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 100\r\n\r\nx"); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-readErr:
		if err == nil {
			t.Fatal("the stalled body read completed without error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stalled body was never cut off")
	}
}

// TestAFinishedBodyLeavesTheHandlerAlone: the deadline is for sending the body,
// not for the work after it, so a handler that runs past it is not cancelled.
func TestAFinishedBodyLeavesTheHandlerAlone(t *testing.T) {
	srv := httptest.NewServer(withBodyReadDeadline(100*time.Millisecond, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		select {
		case <-time.After(400 * time.Millisecond):
		case <-r.Context().Done():
			http.Error(w, "cancelled", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(body)
	})))
	t.Cleanup(srv.Close)

	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := fmt.Fprint(conn, "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 5\r\n\r\nhello"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(got), "hello") {
		t.Fatalf("status %d body %q, want 200 hello", resp.StatusCode, got)
	}
}
