package connectors

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gsoultan/metis/internal/pkg/httpclient"
	"github.com/gsoultan/metis/internal/pkg/idempotency"
	panmail "github.com/gsoultan/panmail-sdk"
)

// PanmailConnectorKey names the built-in connector that sends mail through a
// Panmail gateway.
const PanmailConnectorKey = "panmail-email"

// panmailTransportRetries is how many times a send whose outcome is unknown —
// a dropped connection, a reply cut short — is asked again. Only a send that
// carries the job's idempotency key is ever repeated, and the gateway answers
// a repeated key with the message it already accepted, so a repeat finds out
// what happened rather than sending a second copy.
const panmailTransportRetries = 2

// PanmailConnector is a built-in ConnectorExecutor that sends an email through
// a Panmail gateway.
//
// Config keys: url (required, the gateway's origin), api_key (required, with
// the email:send scope), provider_id (required), from (required), reply_to.
// Payload keys: to (required, comma-separated), cc, bcc, subject (required),
// body (plain text) and html — at least one of the two bodies.
type PanmailConnector struct {
	client *http.Client
}

// NewPanmailConnector creates a PanmailConnector. A nil client sends through
// the shared, egress-guarded client every other connector uses.
func NewPanmailConnector(client *http.Client) *PanmailConnector {
	return &PanmailConnector{client: client}
}

func (c *PanmailConnector) httpClient() *http.Client {
	if c.client != nil {
		return c.client
	}
	return httpclient.Shared()
}

func (c *PanmailConnector) Execute(ctx context.Context, config map[string]any, payload map[string]any) (map[string]any, error) {
	cfg, err := extractPanmailConfig(config)
	if err != nil {
		return nil, err
	}
	msg, err := buildPanmailMessage(cfg, payload)
	if err != nil {
		return nil, err
	}
	// The key the job worker derived for this unit of work, so a job retried
	// after a timeout sends the same message rather than a second one. A call
	// made outside a job — the Connectors page's test — has none, and is then
	// never repeated on an unknown outcome.
	if key, ok := idempotency.KeyFrom(ctx); ok {
		msg.IdempotencyKey = key
	}
	client, err := panmail.New(cfg.url, cfg.apiKey,
		panmail.WithHTTPClient(c.httpClient()),
		panmail.WithTransportRetries(panmailTransportRetries))
	if err != nil {
		return nil, fmt.Errorf("panmail connector: %w", err)
	}
	result, err := client.Send(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("panmail connector: send: %w", ExplainPanmailError(err))
	}
	// "status": "sent" as every other mail connector reports it. It means the
	// gateway has the message, not that it has arrived; message_id is what
	// Panmail's delivery events are keyed by.
	return map[string]any{"status": "sent", "message_id": result.MessageID}, nil
}
