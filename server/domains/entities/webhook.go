package entities

import "github.com/google/uuid"

// DefaultWebhookSignatureHeader is where a signature is looked for when the
// sender's own header has not been named.
//
// There is no standard: GitHub uses X-Hub-Signature-256, Stripe uses
// Stripe-Signature, others invent their own. This is the closest thing to a
// neutral default.
const DefaultWebhookSignatureHeader = "X-Signature-256"

// Webhook is a public address a partner posts events to.
type Webhook struct {
	ID      uuid.UUID `json:"id,omitzero"`
	Project *Project  `json:"project,omitzero"`
	Name    string    `json:"name"`

	// Token appears in the URL and identifies the webhook. It is not a
	// credential — request paths are logged by every proxy in between.
	Token string `json:"token,omitzero"`

	// Secret is returned exactly once, when the webhook is created. It is
	// encrypted at rest and no read path gives it back, because a secret that
	// can be read back is one that will be found in a response log.
	Secret string `json:"secret,omitzero"`

	// SignatureHeader is where the sender puts the signature.
	SignatureHeader string `json:"signature_header,omitzero"`

	// MessageName is the BPMN message a delivery becomes.
	MessageName string `json:"message_name"`

	// CorrelationExpression picks the value that says which instance is
	// waiting, as a FEEL expression over the delivered payload. Empty means
	// every delivery starts a process rather than moving one.
	CorrelationExpression string `json:"correlation_expression,omitzero"`

	Enabled bool `json:"enabled"`
}

// WebhookDelivery is one inbound request, before anything is believed about it.
type WebhookDelivery struct {
	// Token is the address from the URL.
	Token string

	// Signature is the value from the webhook's configured header.
	Signature string

	// DeliveryID is the sender's own ID for this event, used to recognise a
	// retry. Empty when the sender gives none, in which case a retry cannot be
	// told from a new event.
	DeliveryID string

	// Body is the exact bytes delivered. The signature is over these, so they
	// must not be re-encoded on the way here.
	Body []byte
}

// WebhookOutcome is what happened to a delivery.
type WebhookOutcome struct {
	// Duplicate is true when this delivery had already been acted on. The
	// caller answers success anyway: a sender that gets an error retries, and
	// retrying is what produced this.
	Duplicate bool `json:"duplicate,omitzero"`

	MessageName    string `json:"message_name,omitzero"`
	CorrelationKey string `json:"correlation_key,omitzero"`
}
