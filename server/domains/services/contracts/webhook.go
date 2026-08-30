package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// WebhookService receives events partners post, and manages the addresses they
// post them to.
type WebhookService interface {
	// Receive turns one delivery into a BPMN message, if its signature checks
	// out and it is not a retry of something already acted on.
	//
	// It is the only method here reachable without authentication, which is
	// what a webhook is.
	Receive(ctx context.Context, delivery entities.WebhookDelivery) (entities.WebhookOutcome, error)

	// CreateWebhook registers an address, returning its token and — once, here
	// and nowhere else — its secret.
	CreateWebhook(ctx context.Context, webhook entities.Webhook) (entities.Webhook, error)

	// ListWebhooks returns a project's webhooks, without their secrets.
	ListWebhooks(ctx context.Context, projectID uuid.UUID) ([]entities.Webhook, error)

	// SetWebhookEnabled switches a webhook on or off without losing its token.
	SetWebhookEnabled(ctx context.Context, id uuid.UUID, enabled bool) error

	DeleteWebhook(ctx context.Context, id uuid.UUID) error

	// ForgetOldDeliveries drops the delivery records that have outlived any
	// sender's retry schedule.
	ForgetOldDeliveries(ctx context.Context) (int64, error)
}
