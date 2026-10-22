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

	// CloseLegacySignatures stops a webhook accepting legacy signatures from
	// now, once its sender signs with v2. It only ever shortens the window.
	CloseLegacySignatures(ctx context.Context, id uuid.UUID) error

	DeleteWebhook(ctx context.Context, id uuid.UUID) error

	// SimulateWebhookDelivery says what a delivery like this one would do,
	// without doing any of it: no process moves and its delivery ID is not
	// recorded. The webhook's secret checks a signature given with it and is
	// never part of the answer.
	SimulateWebhookDelivery(ctx context.Context, id uuid.UUID, delivery entities.WebhookSimulation) (entities.WebhookSimulationResult, error)

	// ForgetOldDeliveries drops the delivery records that have outlived any
	// sender's retry schedule.
	ForgetOldDeliveries(ctx context.Context) (int64, error)
}
