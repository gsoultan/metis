package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gsoultan/metis/internal/pkg/httpclient"
)

// TeamsConnectorKey names the built-in Microsoft Teams connector.
const TeamsConnectorKey = "ms-teams-message"

// adaptiveCardContentType and adaptiveCardVersion describe the card a Teams
// webhook is sent. Version 1.4 is the newest every Teams client renders.
const (
	adaptiveCardContentType = "application/vnd.microsoft.card.adaptive"
	adaptiveCardSchema      = "http://adaptivecards.io/schemas/adaptive-card.json"
	adaptiveCardVersion     = "1.4"
)

// TeamsConnector posts a message to a Microsoft Teams channel through a
// webhook.
//
// It sends an Adaptive Card. It used to send `{"text": …}`, the body of the
// Office 365 connector webhooks Microsoft has been retiring since 2024. The
// Workflows webhooks that replace them ("Post to a channel when a webhook
// request is received") read only a card attachment, so a plain text body
// posted nothing there. The card is also what the old webhooks accept, so a
// connection still on one keeps working.
//
// Config keys: webhook_url (required).
// Payload keys: text or content (required), title (optional).
type TeamsConnector struct{}

// NewTeamsConnector creates a new TeamsConnector.
func NewTeamsConnector() *TeamsConnector {
	return &TeamsConnector{}
}

func (c *TeamsConnector) Execute(ctx context.Context, config map[string]any, payload map[string]any) (map[string]any, error) {
	webhookURL, _ := TextSetting(config, "webhook_url")
	if webhookURL == "" {
		return nil, fmt.Errorf("teams connector: missing required config key 'webhook_url'")
	}
	text := messageText(payload)
	if text == "" {
		return nil, fmt.Errorf("teams connector: missing required payload key 'text' (or 'content')")
	}
	title, _ := TextSetting(payload, "title")
	body, err := json.Marshal(teamsCardMessage(title, text))
	if err != nil {
		return nil, fmt.Errorf("teams connector: marshal payload: %w", err)
	}
	if err := postToTeams(ctx, webhookURL, body); err != nil {
		return nil, err
	}
	return map[string]any{"status": "sent"}, nil
}

// messageText is a chat message's text, under either name a step may give it.
func messageText(payload map[string]any) string {
	if text, _ := TextSetting(payload, "text"); text != "" {
		return text
	}
	content, _ := TextSetting(payload, "content")
	return content
}

// teamsCardMessage wraps the text, and a bold title when there is one, in the
// message envelope Teams webhooks read.
func teamsCardMessage(title, text string) map[string]any {
	var blocks []map[string]any
	if title != "" {
		blocks = append(blocks, map[string]any{
			"type": "TextBlock", "text": title, "weight": "Bolder", "size": "Medium", "wrap": true,
		})
	}
	blocks = append(blocks, map[string]any{"type": "TextBlock", "text": text, "wrap": true})
	return map[string]any{
		"type": "message",
		"attachments": []map[string]any{{
			"contentType": adaptiveCardContentType,
			"content": map[string]any{
				"$schema": adaptiveCardSchema,
				"type":    "AdaptiveCard",
				"version": adaptiveCardVersion,
				"body":    blocks,
			},
		}},
	}
}

func postToTeams(ctx context.Context, webhookURL string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("teams connector: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpclient.Shared().Do(req)
	if err != nil {
		return fmt.Errorf("teams connector: send request: %w", err)
	}
	defer closeQuietly(resp.Body, "teams")
	// A Workflows webhook answers 202, an Office 365 one 200.
	if resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("teams connector: Teams answered %s", resp.Status)
	}
	return nil
}
