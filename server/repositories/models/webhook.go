package models

import "time"

// WebhookModel is an address a partner can post events to.
//
// It is the inbound half of an integration: a payment processor, a CRM or a
// git host announcing that something happened, correlated to a process waiting
// to hear it. The engine already accepts messages over its authenticated API
// and over AMQP; neither is something a third party's webhook configuration
// screen can talk to, which is why this exists.
//
// The endpoint it backs is public and unauthenticated in the ordinary sense —
// that is what a webhook is — so everything that makes it safe is here: a token
// that only identifies, a secret that authenticates, and a switch to turn it
// off.
type WebhookModel struct {
	Base

	ProjectID UUID   `gorm:"index" json:"project_id,omitzero"`
	Name      string `gorm:"size:255" json:"name"`

	// Token appears in the URL and identifies the webhook. It is not a
	// credential: it travels in request paths, which are logged by every proxy
	// between the sender and here, so it is treated as public. Knowing it lets
	// someone address this webhook and nothing more.
	Token string `gorm:"size:64;uniqueIndex" json:"token"`

	// Secret is the shared key the signature is computed with, encrypted at
	// rest like every other credential. This is the part that authenticates,
	// and it is never returned by any read path.
	Secret string `gorm:"size:512" json:"-"`

	// SignatureHeader is where the sender puts the signature. There is no
	// standard: GitHub uses X-Hub-Signature-256, Stripe uses Stripe-Signature,
	// others invent their own. Naming it per webhook is the difference between
	// supporting one sender and supporting any.
	SignatureHeader string `gorm:"size:128" json:"signature_header"`

	// MessageName is the BPMN message this delivery becomes.
	MessageName string `gorm:"size:255" json:"message_name"`

	// CorrelationExpression picks the value that says which instance is waiting,
	// as a FEEL expression over the delivered payload — `order.id`,
	// `data.object.customer`. Empty means the message is not correlated to one
	// instance, which is how a message start event works.
	CorrelationExpression string `gorm:"size:512" json:"correlation_expression,omitzero"`

	// Enabled is the switch. A webhook that is misbehaving needs to be stopped
	// without deleting it, because deleting it loses the token and the sender
	// has to be reconfigured.
	Enabled bool `gorm:"default:true" json:"enabled"`
}

// TableName overrides the table name for WebhookModel.
func (WebhookModel) TableName() string {
	return "webhooks"
}

// WebhookDeliveryModel records a delivery that has been accepted.
//
// Senders retry. A partner that does not get a 2xx in time sends the same event
// again — often several times, sometimes for hours — and every one of those is
// a message that would start another process or move an existing one twice. The
// senders that retry are also the ones that give each event a stable ID for
// exactly this purpose.
//
// So a delivery is remembered, and a second one carrying an ID already seen is
// answered with the same success rather than acted on. The unique index is what
// makes that safe under concurrent deliveries: the insert is the claim.
type WebhookDeliveryModel struct {
	Base

	WebhookID UUID `gorm:"index;uniqueIndex:ux_webhook_deliveries_identity,priority:1" json:"webhook_id,omitzero"`

	// DeliveryID is the sender's ID for this event.
	DeliveryID string `gorm:"size:191;uniqueIndex:ux_webhook_deliveries_identity,priority:2" json:"delivery_id"`

	// ReceivedAt is what the retention sweep works from: these rows exist to
	// answer "have I seen this?" for as long as a sender might retry, and are
	// worthless after that.
	ReceivedAt time.Time `gorm:"index" json:"received_at"`
}

// TableName overrides the table name for WebhookDeliveryModel.
func (WebhookDeliveryModel) TableName() string {
	return "webhook_deliveries"
}
