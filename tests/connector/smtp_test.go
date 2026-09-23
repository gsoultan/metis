package connector_test

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"

	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// The SMTP connector, against a server that speaks SMTP.
//
// Everything else that touches this executor proves it refuses an incomplete
// configuration and survives a payload of the wrong type. Nothing proved an
// email was ever handed over, or what was in it — so the envelope and the
// message could have been wrong in any way at all and the suite would have been
// green.
//
// The server here is in-process rather than a container: SMTP is small enough
// to answer honestly in sixty lines, and a hermetic test is one that always
// runs. That matters because CI fails on any skipped test — a gated suite that
// nobody wired up is a suite that reports success having checked nothing.

// recordedMail is what the fake server saw.
type recordedMail struct {
	from string
	to   []string
	data string
}

// fakeSMTP accepts one connection, speaks enough SMTP for net/smtp to complete a
// SendMail, and records the envelope and body.
//
// It deliberately does not advertise STARTTLS. net/smtp upgrades whenever the
// server offers it, and this has no certificate; refusing to offer it is how the
// exchange stays plain. PlainAuth then only agrees to send credentials because
// the host is "localhost" — which is why the test dials by name rather than by
// address.
func fakeSMTP(t *testing.T) (addr string, received func() recordedMail) {
	t.Helper()

	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	var (
		once sync.Once
		mail recordedMail
		done = make(chan struct{})
	)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			close(done)
			return
		}
		defer func() { _ = conn.Close() }()

		r := bufio.NewReader(conn)
		w := bufio.NewWriter(conn)
		say := func(line string) {
			_, _ = w.WriteString(line + "\r\n")
			_ = w.Flush()
		}

		say("220 localhost ESMTP metis-test")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				break
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				say("250-localhost")
				say("250 AUTH PLAIN")
			case strings.HasPrefix(cmd, "HELO"):
				say("250 localhost")
			case strings.HasPrefix(cmd, "AUTH"):
				say("235 2.7.0 Authentication successful")
			case strings.HasPrefix(cmd, "MAIL FROM"):
				mail.from = between(line, "<", ">")
				say("250 2.1.0 Ok")
			case strings.HasPrefix(cmd, "RCPT TO"):
				mail.to = append(mail.to, between(line, "<", ">"))
				say("250 2.1.5 Ok")
			case cmd == "DATA":
				say("354 End data with <CR><LF>.<CR><LF>")
				var body strings.Builder
				for {
					dataLine, err := r.ReadString('\n')
					if err != nil {
						break
					}
					if strings.TrimRight(dataLine, "\r\n") == "." {
						break
					}
					body.WriteString(dataLine)
				}
				mail.data = body.String()
				say("250 2.0.0 Ok: queued")
			case cmd == "QUIT":
				say("221 2.0.0 Bye")
				once.Do(func() { close(done) })
				return
			default:
				say("250 2.0.0 Ok")
			}
		}
		once.Do(func() { close(done) })
	}()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split the listener address: %v", err)
	}

	return port, func() recordedMail {
		<-done
		return mail
	}
}

func between(s, open, close string) string {
	i := strings.Index(s, open)
	j := strings.LastIndex(s, close)
	if i < 0 || j < 0 || j <= i {
		return ""
	}
	return s[i+len(open) : j]
}

// TestSMTPConnectorHandsOverTheEmail is the claim, asserted: the envelope names
// the configured sender and the payload's recipient, and the message carries the
// subject and body the process built.
func TestSMTPConnectorHandsOverTheEmail(t *testing.T) {
	port, received := fakeSMTP(t)

	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestConn(t)))
	result, err := svc.ExecuteConnector(context.Background(), "email-smtp",
		map[string]any{
			"host":     "localhost",
			"port":     port,
			"username": "metis",
			"password": "secret",
			"from":     "noreply@example.com",
		},
		map[string]any{
			"to":      "approver@example.com",
			"subject": "Claim C-42 needs approval",
			"body":    "The claim is over the limit.",
		})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if result["status"] != "sent" {
		t.Errorf("status: got %v, want sent", result["status"])
	}

	mail := received()
	if mail.from != "noreply@example.com" {
		t.Errorf("envelope sender: got %q, want the configured from address", mail.from)
	}
	if len(mail.to) != 1 || mail.to[0] != "approver@example.com" {
		t.Errorf("envelope recipients: got %v, want [approver@example.com]", mail.to)
	}
	if !strings.Contains(mail.data, "Subject: Claim C-42 needs approval") {
		t.Errorf("the message carries no subject header:\n%s", mail.data)
	}
	if !strings.Contains(mail.data, "The claim is over the limit.") {
		t.Errorf("the message carries no body:\n%s", mail.data)
	}
}

// TestSMTPConnectorDoesNotReportAnUndeliveredEmailAsSent covers the refusal. A
// server that rejects the recipient must not leave the process believing the
// approver was told.
func TestSMTPConnectorDoesNotReportAnUndeliveredEmailAsSent(t *testing.T) {
	// No listener at all: the nearest thing to a mail server that is down. A
	// port is taken and released so nothing else can be listening on it.
	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	if err := listener.Close(); err != nil {
		t.Fatalf("close the listener: %v", err)
	}

	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestConn(t)))
	_, err = svc.ExecuteConnector(context.Background(), "email-smtp",
		map[string]any{
			"host": "localhost", "port": port,
			"username": "metis", "password": "secret", "from": "noreply@example.com",
		},
		map[string]any{"to": "approver@example.com", "subject": "s", "body": "b"})

	if err == nil {
		t.Fatal("an email that was never handed to a server was reported as sent")
	}
}

// TestSMTPConnectorSendsToEveryAddressItWasGiven.
//
// The connector documents 'to' as a comma-separated list, and the envelope has
// to carry all of them: an address that only reaches the header is an address
// the mail server never delivers to.
func TestSMTPConnectorSendsToEveryAddressItWasGiven(t *testing.T) {
	port, received := fakeSMTP(t)

	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestConn(t)))
	_, err := svc.ExecuteConnector(context.Background(), "email-smtp",
		map[string]any{
			"host": "localhost",
			"port": port,
			"from": "noreply@example.com",
		},
		map[string]any{
			// Spacing and a trailing separator, because a list typed by a
			// person has both.
			"to":      "approver@example.com, cfo@example.com ,",
			"subject": "Claim C-42 needs approval",
			"body":    "The claim is over the limit.",
		})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	mail := received()
	if len(mail.to) != 2 || mail.to[0] != "approver@example.com" || mail.to[1] != "cfo@example.com" {
		t.Errorf("envelope recipients: got %v, want both addresses and nothing empty", mail.to)
	}
}
