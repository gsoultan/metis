package connectors

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	netmail "net/mail"
	"slices"
	"strings"
	"testing"
)

func parseBuilt(t *testing.T, payload map[string]any) ([]string, *netmail.Message) {
	t.Helper()
	cfg := smtpConfig{host: "smtp.example.com", port: "587", from: "noreply@example.com"}
	recipients, msg, err := buildEmailMessage(cfg, payload)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	parsed, err := netmail.ReadMessage(bytes.NewReader(msg))
	if err != nil {
		t.Fatalf("the message does not parse: %v\n%s", err, msg)
	}
	return recipients, parsed
}

// Bcc reaches its recipients on the envelope and nobody reads it in a header.
func TestCcAndBccReachTheirRecipients(t *testing.T) {
	recipients, parsed := parseBuilt(t, map[string]any{
		"to": "ada@example.com", "cc": "bob@example.com, cy@example.com", "bcc": "audit@example.com",
		"subject": "PO-17 approved",
	})
	want := []string{"ada@example.com", "bob@example.com", "cy@example.com", "audit@example.com"}
	if !slices.Equal(recipients, want) {
		t.Errorf("envelope = %v, want %v", recipients, want)
	}
	if got := parsed.Header.Get("Cc"); got != "bob@example.com, cy@example.com" {
		t.Errorf("Cc = %q", got)
	}
	if got := parsed.Header.Get("Bcc"); got != "" {
		t.Errorf("Bcc was written into the headers: %q", got)
	}
}

func TestALineBreakInCcIsRefused(t *testing.T) {
	cfg := smtpConfig{host: "smtp.example.com", port: "587", from: "noreply@example.com"}
	_, _, err := buildEmailMessage(cfg, map[string]any{
		"to": "ada@example.com", "cc": "bob@example.com\r\nBcc: x@example.net", "subject": "s",
	})
	if err == nil || !strings.Contains(err.Error(), "'cc'") {
		t.Errorf("error = %v, want the line break in cc refused", err)
	}
}

// Date and Message-ID are what a receiving server expects; their absence
// counts against a message.
func TestAMessageCarriesADateAndAMessageID(t *testing.T) {
	_, parsed := parseBuilt(t, map[string]any{"to": "ada@example.com", "subject": "s"})
	if _, err := parsed.Header.Date(); err != nil {
		t.Errorf("Date: %v", err)
	}
	if id := parsed.Header.Get("Message-ID"); !strings.HasSuffix(id, "@example.com>") {
		t.Errorf("Message-ID = %q, want one in the sender's domain", id)
	}
}

func TestASubjectThatIsNotASCIIReadsAsWritten(t *testing.T) {
	_, parsed := parseBuilt(t, map[string]any{"to": "ada@example.com", "subject": "Persetujuan PO – Müller"})
	decoded, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || decoded != "Persetujuan PO – Müller" {
		t.Errorf("Subject decodes to %q (%v)", decoded, err)
	}
}

// With HTML the message carries both, and a client shows whichever it can.
func TestAnHTMLMessageKeepsItsTextPart(t *testing.T) {
	_, parsed := parseBuilt(t, map[string]any{
		"to": "ada@example.com", "subject": "s", "body": "Approve PO-17", "html": "<p>Approve <b>PO-17</b></p>",
	})
	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("Content-Type = %q (%v)", parsed.Header.Get("Content-Type"), err)
	}
	reader := multipart.NewReader(parsed.Body, params["boundary"])
	var types []string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("part: %v", err)
		}
		types = append(types, strings.Split(part.Header.Get("Content-Type"), ";")[0])
	}
	if !slices.Equal(types, []string{"text/plain", "text/html"}) {
		t.Errorf("parts = %v, want text then HTML", types)
	}
}
