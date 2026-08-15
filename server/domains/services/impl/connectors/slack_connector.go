package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gsoultan/gobpm/internal/pkg/httpclient"
)

const SlackConnectorKey = "slack-message"

// slackPayload is the Slack Incoming Webhook request body.
type slackPayload struct {
	Text    string `json:"text"`
	Channel string `json:"channel,omitempty"`
}

// SlackConnector is a built-in ConnectorExecutor that posts a message to Slack
// via an Incoming Webhook URL.
// Config keys: webhook_url (required).
// Payload keys: text (required), channel (optional, overrides webhook default).
type SlackConnector struct{}

// NewSlackConnector creates a new SlackConnector.
func NewSlackConnector() *SlackConnector {
	return &SlackConnector{}
}

func (c *SlackConnector) Execute(ctx context.Context, config map[string]any, payload map[string]any) (map[string]any, error) {
	webhookURL, err := extractWebhookURL(config)
	if err != nil {
		return nil, err
	}
	body, err := buildSlackBody(payload)
	if err != nil {
		return nil, err
	}
	if err := postToSlack(ctx, webhookURL, body); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

func extractWebhookURL(config map[string]any) (string, error) {
	url, _ := config["webhook_url"].(string)
	if url == "" {
		return "", fmt.Errorf("slack connector: missing required config key 'webhook_url'")
	}
	return url, nil
}

func buildSlackBody(payload map[string]any) ([]byte, error) {
	text, _ := payload["text"].(string)
	if text == "" {
		return nil, fmt.Errorf("slack connector: missing required payload key 'text'")
	}
	channel, _ := payload["channel"].(string)
	data, err := json.Marshal(slackPayload{Text: text, Channel: channel})
	if err != nil {
		return nil, fmt.Errorf("slack connector: marshal payload: %w", err)
	}
	return data, nil
}

func postToSlack(ctx context.Context, webhookURL string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("slack connector: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpclient.Shared().Do(req)
	if err != nil {
		return fmt.Errorf("slack connector: send request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack connector: unexpected status %d", resp.StatusCode)
	}
	return nil
}
