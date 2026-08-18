package webhook

import "github.com/gsoultan/gobpm/server/domains/entities"

// ListWebhooksRequest asks for a project's registered addresses.
type ListWebhooksRequest struct {
	ProjectID string `json:"project_id"`
}

// ListWebhooksResponse carries them, without their secrets.
type ListWebhooksResponse struct {
	Webhooks []entities.Webhook `json:"webhooks,omitzero"`
	Err      error              `json:"err,omitzero"`
}

func (r ListWebhooksResponse) Failed() error { return r.Err }

// CreateWebhookRequest registers an address.
//
// The token and the secret are not accepted from the caller: one someone chose
// is one someone can guess, and the other is usually short.
type CreateWebhookRequest struct {
	ProjectID             string `json:"project_id"`
	Name                  string `json:"name"`
	MessageName           string `json:"message_name"`
	CorrelationExpression string `json:"correlation_expression,omitzero"`
	SignatureHeader       string `json:"signature_header,omitzero"`
}

// CreateWebhookResponse is the only place the secret ever appears.
type CreateWebhookResponse struct {
	Webhook entities.Webhook `json:"webhook,omitzero"`
	Err     error            `json:"err,omitzero"`
}

func (r CreateWebhookResponse) Failed() error { return r.Err }

// SetWebhookEnabledRequest switches one on or off.
type SetWebhookEnabledRequest struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

// SetWebhookEnabledResponse reports the outcome.
type SetWebhookEnabledResponse struct {
	Err error `json:"err,omitzero"`
}

func (r SetWebhookEnabledResponse) Failed() error { return r.Err }

// DeleteWebhookRequest removes an address, and with it its token.
type DeleteWebhookRequest struct {
	ID string `json:"id"`
}

// DeleteWebhookResponse reports the outcome.
type DeleteWebhookResponse struct {
	Err error `json:"err,omitzero"`
}

func (r DeleteWebhookResponse) Failed() error { return r.Err }
