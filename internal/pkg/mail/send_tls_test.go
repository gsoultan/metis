package mail

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// smtpsServer is a server on an implicit-TLS port: TLS from the first byte,
// then just enough SMTP to take one message. It returns its address, the pool
// that trusts its certificate, and the message it was given.
func smtpsServer(t *testing.T) (string, *x509.CertPool, <-chan string) {
	t.Helper()
	certSource := httptest.NewTLSServer(http.NotFoundHandler())
	config := certSource.TLS.Clone()
	pool := x509.NewCertPool()
	pool.AddCert(certSource.Certificate())
	certSource.Close()

	listener, err := tls.Listen("tcp", "127.0.0.1:0", config)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	received := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		received <- converse(conn)
	}()
	return listener.Addr().String(), pool, received
}

// converse plays the server's side of one delivery and returns the message.
func converse(conn net.Conn) string {
	reader := bufio.NewReader(conn)
	write := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
	write("220 smtps.example.com ESMTP")
	var message strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return message.String()
		}
		switch command := strings.ToUpper(strings.TrimSpace(line)); {
		case strings.HasPrefix(command, "EHLO"), strings.HasPrefix(command, "HELO"):
			write("250 smtps.example.com")
		case strings.HasPrefix(command, "DATA"):
			write("354 go ahead")
			for {
				data, err := reader.ReadString('\n')
				if err != nil || data == ".\r\n" {
					break
				}
				message.WriteString(data)
			}
			write("250 queued")
		case strings.HasPrefix(command, "QUIT"):
			write("221 bye")
			return message.String()
		default:
			write("250 ok")
		}
	}
}

// A server on 465 speaks TLS first. Send dialled it in plain text and waited
// for a greeting until its deadline.
func TestAMessageReachesAServerThatSpeaksTLSFirst(t *testing.T) {
	addr, pool, received := smtpsServer(t)

	err := send(t.Context(), addr, &tls.Config{ServerName: "example.com", RootCAs: pool, MinVersion: tls.VersionTLS12},
		nil, "noreply@example.com", []string{"ada@example.com"}, []byte("Subject: hi\r\n\r\nhello over TLS\r\n"))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if message := <-received; !strings.Contains(message, "hello over TLS") {
		t.Errorf("the server was given %q", message)
	}
}
