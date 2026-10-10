package connectors

import (
	"bytes"
	"io"
	netmail "net/mail"
	"strings"
	"testing"
)

// The subject is process data. Written as it came, a line break in it ended
// the Subject header and began whatever the author wrote next — a Bcc, or a
// blank line and a body of their choosing.
func TestALineBreakInTheSubjectDoesNotStartAnotherHeader(t *testing.T) {
	cfg := smtpConfig{host: "smtp.example.com", port: "587", from: "noreply@example.com"}
	_, msg, err := buildEmailMessage(cfg, map[string]any{
		"to":      "ollie@example.com",
		"subject": "Invoice ready\r\nBcc: attacker@example.net\r\n\r\nPay to account 666",
		"body":    "The real body.",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	parsed, err := netmail.ReadMessage(bytes.NewReader(msg))
	if err != nil {
		t.Fatalf("the message does not parse: %v\n%s", err, msg)
	}
	if bcc := parsed.Header.Get("Bcc"); bcc != "" {
		t.Fatalf("the subject injected a Bcc header: %q", bcc)
	}
	if got, want := parsed.Header.Get("Subject"), "Invoice ready Bcc: attacker@example.net Pay to account 666"; got != want {
		t.Fatalf("Subject = %q, want %q", got, want)
	}
	body, err := io.ReadAll(parsed.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "The real body." {
		t.Fatalf("body = %q; the subject reached into it", body)
	}
}

// The To header is written as given. A line break between two addresses
// survives the trimming that keeps each valid on the envelope, so it would end
// the header and start another; it is refused instead.
func TestALineBreakInTheRecipientsIsRefused(t *testing.T) {
	cfg := smtpConfig{host: "smtp.example.com", port: "587", from: "noreply@example.com"}
	_, _, err := buildEmailMessage(cfg, map[string]any{
		"to":      "ollie@example.com,\r\nX-Injected: yes",
		"subject": "Hello",
	})
	if err == nil || !strings.Contains(err.Error(), "line break") {
		t.Fatalf("err = %v, want the line break refused", err)
	}
}
