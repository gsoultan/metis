package connectors

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"strings"
	"time"

	"github.com/gsoultan/metis/internal/pkg/mail"
)

// mimeBoundaryBytes is how much randomness separates a message's text part
// from its HTML part, and names its Message-ID.
const mimeBoundaryBytes = 16

// buildEmailMessage returns the envelope recipients and the message.
//
// The two are returned together because they have to agree: a "To:" header
// naming one address and an envelope naming another is a mail that arrives
// somewhere nobody looked for it, and SMTP reports that as a success. Bcc is
// the one deliberate difference: on the envelope, never in a header.
func buildEmailMessage(cfg smtpConfig, payload map[string]any) ([]string, []byte, error) {
	to, err := addressList(payload, "to", true)
	if err != nil {
		return nil, nil, err
	}
	cc, err := addressList(payload, "cc", false)
	if err != nil {
		return nil, nil, err
	}
	bcc, err := addressList(payload, "bcc", false)
	if err != nil {
		return nil, nil, err
	}
	subject, _ := textSetting(payload, "subject")
	if subject == "" {
		return nil, nil, fmt.Errorf("email connector: missing required payload key 'subject'")
	}
	body, _ := textSetting(payload, "body")
	html, _ := textSetting(payload, "html")

	random, err := randomToken()
	if err != nil {
		return nil, nil, err
	}
	var sb strings.Builder
	writeHeaders(&sb, cfg.from, to, cc, subject, random)
	writeBody(&sb, body, html, random)
	recipients := append(append(append([]string{}, to...), cc...), bcc...)
	return recipients, []byte(sb.String()), nil
}

// addressList reads a comma-separated list of addresses.
//
// A line break is refused, not folded. The list is written into a header, and
// a line break between two addresses survives the trimming that keeps each
// one valid on the envelope — so it would end the header and start another.
func addressList(payload map[string]any, key string, required bool) ([]string, error) {
	raw, _ := textSetting(payload, key)
	if raw == "" && !required {
		return nil, nil
	}
	if raw == "" {
		return nil, fmt.Errorf("email connector: missing required payload key '%s'", key)
	}
	if strings.ContainsAny(raw, "\r\n") {
		return nil, fmt.Errorf("email connector: payload key '%s' contains a line break", key)
	}
	addresses := splitRecipients(raw)
	if len(addresses) == 0 {
		return nil, fmt.Errorf("email connector: payload key '%s' names no address", key)
	}
	return addresses, nil
}

// splitRecipients reads the comma-separated list the payload documents.
func splitRecipients(list string) []string {
	parts := strings.Split(list, ",")
	recipients := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			recipients = append(recipients, trimmed)
		}
	}
	return recipients
}

// writeHeaders writes the headers a receiving server expects. Date and
// Message-ID were missing, and their absence counts against a message in
// most spam filters. A subject that is not plain ASCII is encoded, so it
// reads as written rather than as mojibake.
func writeHeaders(sb *strings.Builder, from string, to, cc []string, subject, random string) {
	sb.WriteString("From: " + from + "\r\n")
	sb.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	if len(cc) > 0 {
		sb.WriteString("Cc: " + strings.Join(cc, ", ") + "\r\n")
	}
	sb.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", mail.HeaderText(subject)) + "\r\n")
	sb.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	sb.WriteString("Message-ID: <" + random + "@" + messageIDDomain(from) + ">\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
}

// writeBody writes the text, and the HTML beside it when there is some, as
// the two parts of one message a mail client picks between.
func writeBody(sb *strings.Builder, text, html, boundary string) {
	if html == "" {
		sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		sb.WriteString(text)
		return
	}
	sb.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")
	sb.WriteString("--" + boundary + "\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + text + "\r\n")
	sb.WriteString("--" + boundary + "\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n" + html + "\r\n")
	sb.WriteString("--" + boundary + "--\r\n")
}

// messageIDDomain is the sender's domain, which is what a Message-ID names.
func messageIDDomain(from string) string {
	if _, domain, found := strings.Cut(strings.TrimSuffix(from, ">"), "@"); found && domain != "" {
		return domain
	}
	return "metis.invalid"
}

func randomToken() (string, error) {
	raw := make([]byte, mimeBoundaryBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("email connector: could not generate a message id: %w", err)
	}
	return hex.EncodeToString(raw), nil
}
