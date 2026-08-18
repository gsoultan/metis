package connectors

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
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

func (c *EmailConnector) Execute(_ context.Context, config map[string]any, payload map[string]any) (map[string]any, error) {
	cfg, err := extractSMTPConfig(config)
	if err != nil {
		return nil, err
	}
	msg, err := buildEmailMessage(cfg, payload)
	if err != nil {
		return nil, err
	}
	if err := sendEmail(cfg, msg); err != nil {
		return nil, err
	}
	return map[string]any{"sent": true}, nil
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
	port, _ := textSetting(config, "port")
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

func buildEmailMessage(cfg smtpConfig, payload map[string]any) ([]byte, error) {
	to, _ := textSetting(payload, "to")
	if to == "" {
		return nil, fmt.Errorf("email connector: missing required payload key 'to'")
	}
	subject, _ := textSetting(payload, "subject")
	if subject == "" {
		return nil, fmt.Errorf("email connector: missing required payload key 'subject'")
	}
	body, _ := payload["body"].(string)

	var sb strings.Builder
	sb.WriteString("From: " + cfg.from + "\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	sb.WriteString(body)
	return []byte(sb.String()), nil
}

func sendEmail(cfg smtpConfig, msg []byte) error {
	addr := cfg.host + ":" + cfg.port
	var auth smtp.Auth
	if cfg.username != "" {
		auth = smtp.PlainAuth("", cfg.username, cfg.password, cfg.host)
	}
	if err := smtp.SendMail(addr, auth, cfg.from, []string{cfg.from}, msg); err != nil {
		return fmt.Errorf("email connector: send mail: %w", err)
	}
	return nil
}
