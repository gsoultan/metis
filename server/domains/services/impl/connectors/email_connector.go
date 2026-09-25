package connectors

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"

	"github.com/gsoultan/metis/internal/pkg/mail"
)

const EmailConnectorKey = "email-smtp"

// EmailConnector is a built-in ConnectorExecutor that sends an email via SMTP.
// Config keys: host (required), port (default 587), username, password, from.
// Payload keys: to (required, comma-separated), subject (required), body (required).
type EmailConnector struct{}

// NewEmailConnector creates a new EmailConnector.
func NewEmailConnector() *EmailConnector {
	return &EmailConnector{}
}

func (c *EmailConnector) Execute(ctx context.Context, config map[string]any, payload map[string]any) (map[string]any, error) {
	cfg, err := extractSMTPConfig(config)
	if err != nil {
		return nil, err
	}
	recipients, msg, err := buildEmailMessage(cfg, payload)
	if err != nil {
		return nil, err
	}
	if err := sendEmail(ctx, cfg, recipients, msg); err != nil {
		return nil, err
	}
	// "status": "sent" rather than "sent": true, because that is what the
	// executor this one replaced returned and what everything reading the
	// result of a connector call expects.
	return map[string]any{"status": "sent"}, nil
}

type smtpConfig struct {
	host     string
	port     string
	username string
	password string
	from     string
}

func extractSMTPConfig(config map[string]any) (smtpConfig, error) {
	host, _ := textSetting(config, "host")
	if host == "" {
		return smtpConfig{}, fmt.Errorf("email connector: missing required config key 'host'")
	}
	port := PortSetting(config, "port")
	if port == "" {
		port = "587"
	}
	username, _ := textSetting(config, "username")
	password, _ := textSetting(config, "password")
	from, _ := textSetting(config, "from")
	if from == "" {
		from = username
	}
	return smtpConfig{host: host, port: port, username: username, password: password, from: from}, nil
}

// buildEmailMessage returns the envelope recipients and the message.
//
// The two are returned together because they have to agree: a "To:" header
// naming one address and an envelope naming another is a mail that arrives
// somewhere nobody looked for it, and SMTP reports that as a success.
func buildEmailMessage(cfg smtpConfig, payload map[string]any) ([]string, []byte, error) {
	to, _ := textSetting(payload, "to")
	if to == "" {
		return nil, nil, fmt.Errorf("email connector: missing required payload key 'to'")
	}
	recipients := splitRecipients(to)
	if len(recipients) == 0 {
		return nil, nil, fmt.Errorf("email connector: payload key 'to' names no address")
	}
	subject, _ := textSetting(payload, "subject")
	if subject == "" {
		return nil, nil, fmt.Errorf("email connector: missing required payload key 'subject'")
	}
	body, _ := textSetting(payload, "body")

	var sb strings.Builder
	sb.WriteString("From: " + cfg.from + "\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	sb.WriteString(body)
	return recipients, []byte(sb.String()), nil
}

// splitRecipients reads the comma-separated list the payload documents.
func splitRecipients(to string) []string {
	parts := strings.Split(to, ",")
	recipients := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			recipients = append(recipients, trimmed)
		}
	}
	return recipients
}

// sendEmail hands the message to the SMTP server.
//
// The envelope recipients are the addresses the payload asked for. They used to
// be cfg.from — the *sender* — so every message a process sent was delivered to
// the configured mailbox rather than to the person it was addressed to, while
// the "To:" header said otherwise and SMTP reported success. An approval
// request for approver@example.com arrived in the noreply@ inbox and nothing
// anywhere looked wrong.
//
// Within the job's deadline: net/smtp's SendMail has none, and a mail server
// that accepted the connection and went quiet held a worker slot for good.
func sendEmail(ctx context.Context, cfg smtpConfig, recipients []string, msg []byte) error {
	addr := cfg.host + ":" + cfg.port
	var auth smtp.Auth
	if cfg.username != "" {
		auth = smtp.PlainAuth("", cfg.username, cfg.password, cfg.host)
	}
	if err := mail.Send(ctx, addr, auth, cfg.from, recipients, msg); err != nil {
		return fmt.Errorf("email connector: send mail: %w", err)
	}
	return nil
}
