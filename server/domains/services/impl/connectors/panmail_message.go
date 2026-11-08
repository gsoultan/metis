package connectors

import (
	"fmt"
	"strings"

	panmail "github.com/gsoultan/panmail-sdk"
)

// panmailConfig is a Panmail connection's settings.
type panmailConfig struct {
	url        string
	apiKey     string
	providerID string
	from       string
	replyTo    string
}

func extractPanmailConfig(config map[string]any) (panmailConfig, error) {
	cfg := panmailConfig{}
	required := []struct {
		key    string
		target *string
	}{
		{"url", &cfg.url},
		{"api_key", &cfg.apiKey},
		{"provider_id", &cfg.providerID},
		{"from", &cfg.from},
	}
	for _, setting := range required {
		value, _ := textSetting(config, setting.key)
		*setting.target = strings.TrimSpace(value)
		if *setting.target == "" {
			return panmailConfig{}, fmt.Errorf("panmail connector: missing required config key '%s'", setting.key)
		}
	}
	replyTo, _ := textSetting(config, "reply_to")
	cfg.replyTo = strings.TrimSpace(replyTo)
	if err := refuseLineBreaks(map[string]string{"from": cfg.from, "reply_to": cfg.replyTo}); err != nil {
		return panmailConfig{}, err
	}
	return cfg, nil
}

// buildPanmailMessage reads the step's payload into the message to send.
func buildPanmailMessage(cfg panmailConfig, payload map[string]any) (panmail.Message, error) {
	to, _ := textSetting(payload, "to")
	cc, _ := textSetting(payload, "cc")
	bcc, _ := textSetting(payload, "bcc")
	// The gateway writes these into headers. Refused here, as the SMTP
	// connector refuses them, rather than trusting every gateway version to.
	if err := refuseLineBreaks(map[string]string{"to": to, "cc": cc, "bcc": bcc}); err != nil {
		return panmail.Message{}, err
	}
	recipients := splitRecipients(to)
	if len(recipients) == 0 {
		return panmail.Message{}, fmt.Errorf("panmail connector: missing required payload key 'to'")
	}
	subject, _ := textSetting(payload, "subject")
	// Folded, not refused: a subject is process data, and a line break in it
	// is far more often a pasted title than an attack. Either way it never
	// reaches the header as a line break.
	subject = strings.Join(strings.Fields(subject), " ")
	if subject == "" {
		return panmail.Message{}, fmt.Errorf("panmail connector: missing required payload key 'subject'")
	}
	text, _ := textSetting(payload, "body")
	html, _ := textSetting(payload, "html")
	if text == "" && html == "" {
		return panmail.Message{}, fmt.Errorf("panmail connector: the payload needs a 'body', an 'html', or both")
	}
	return panmail.Message{
		ProviderID: cfg.providerID,
		From:       cfg.from,
		ReplyTo:    cfg.replyTo,
		To:         recipients,
		Cc:         splitRecipients(cc),
		Bcc:        splitRecipients(bcc),
		Subject:    subject,
		Text:       text,
		HTML:       html,
	}, nil
}

// refuseLineBreaks reports the first address field holding a carriage return
// or a line feed.
func refuseLineBreaks(fields map[string]string) error {
	for key, value := range fields {
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("panmail connector: '%s' contains a line break", key)
		}
	}
	return nil
}
