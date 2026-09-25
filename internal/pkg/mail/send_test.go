package mail

import (
	"context"
	"net"
	"testing"
	"time"
)

// A server that accepts the connection and never greets is the one that used
// to hold a caller indefinitely: net/smtp waits for the greeting with no
// deadline at all.
func TestASilentServerDoesNotHoldTheCallerPastItsDeadline(t *testing.T) {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		// Say nothing until the test is over.
		<-t.Context().Done()
		_ = conn.Close()
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = Send(ctx, listener.Addr().String(), nil, "noreply@example.com", []string{"ada@example.com"}, []byte("Subject: hi\r\n\r\nhi"))
	if err == nil {
		t.Fatal("a server that never answered was reported as having taken the message")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the send waited %s on a silent server; its deadline was 300ms", elapsed)
	}
}
