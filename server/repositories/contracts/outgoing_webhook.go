package contracts

import (
	"context"

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
}
