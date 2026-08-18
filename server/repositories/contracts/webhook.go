package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

// WebhookRepository stores the addresses partners post events to.
type WebhookRepository interface {
	// GetByToken resolves the token in a delivery's URL, with the secret
	// decrypted. It is not tenant-scoped: it runs before anyone is
	// authenticated, and the row it returns is what establishes the tenant.
	GetByToken(ctx context.Context, token string) (models.WebhookModel, error)

	// ListByProject returns a project's webhooks with their secrets blanked.
	ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.WebhookModel, error)

	Create(ctx context.Context, webhook models.WebhookModel) error

	// SetEnabled switches a webhook on or off. A misbehaving one has to be
	// stoppable without deleting it: deleting loses the token, and the sender
	// has to be reconfigured.
	SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error
	Delete(ctx context.Context, id uuid.UUID) error

	// ClaimDelivery records a delivery and reports whether it is the first one
	// with that ID. A false means the sender is retrying something already
	// acted on.
	ClaimDelivery(ctx context.Context, webhookID uuid.UUID, deliveryID string) (bool, error)

	// ForgetDeliveriesBefore drops delivery records older than the cutoff.
	ForgetDeliveriesBefore(ctx context.Context, cutoff time.Time) (int64, error)
}
