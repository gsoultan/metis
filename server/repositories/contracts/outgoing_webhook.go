package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// OutgoingWebhookRepository stores the addresses a project posts its events
// to. Every method is scoped to the caller's organization: a webhook of
// somebody else's is answered as one that is not there.
type OutgoingWebhookRepository interface {
	// Create records a webhook, sealing its secret. The returned webhook
	// carries the secret it was given, in the clear, for the one response that
	// shows it.
	Create(ctx context.Context, webhook entities.OutgoingWebhook) (entities.OutgoingWebhook, error)

	// ListByProject returns a project's webhooks, oldest first, without their
	// secrets.
	ListByProject(ctx context.Context, projectID uuid.UUID) ([]entities.OutgoingWebhook, error)

	// SetEnabled switches a webhook on or off. Deliveries owed to one that is
	// off wait, rather than being lost, until it is switched on again.
	SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error

	// Delete removes a webhook. Its log stays readable until the retention
	// sweep removes it; what it was still owed is never sent.
	Delete(ctx context.Context, id uuid.UUID) error

	// Signing reads a webhook with its secret unsealed, for a test sent by
	// hand. Nothing it returns is shown to the caller.
	Signing(ctx context.Context, id uuid.UUID) (entities.OutgoingWebhook, error)

	// Deliveries reads a page of a webhook's log, newest first.
	Deliveries(ctx context.Context, webhookID uuid.UUID, filter entities.OutgoingWebhookLogFilter) ([]entities.OutgoingWebhookLogEntry, error)

	// RetryDelivery makes a pending or failed delivery due at now. One that
	// failed is given one more attempt; its count is not reset.
	RetryDelivery(ctx context.Context, deliveryID uuid.UUID, now time.Time) error

	// StopDelivery fails a pending delivery at once, saying why.
	StopDelivery(ctx context.Context, deliveryID uuid.UUID, now time.Time, reason string) error

	// RecordTest logs a delivery sent by hand, settled as it came out.
	RecordTest(ctx context.Context, webhookID uuid.UUID, test entities.OutgoingWebhookTest) error
}
