package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// OutgoingWebhookService manages the addresses a project posts its events to.
type OutgoingWebhookService interface {
	// CreateOutgoingWebhook registers an address for the events named,
	// returning its secret once, here and nowhere else.
	CreateOutgoingWebhook(ctx context.Context, webhook entities.OutgoingWebhook) (entities.OutgoingWebhook, error)

	// ListOutgoingWebhooks returns a project's outgoing webhooks, without
	// their secrets.
	ListOutgoingWebhooks(ctx context.Context, projectID uuid.UUID) ([]entities.OutgoingWebhook, error)

	// SetOutgoingWebhookEnabled switches one on or off. What it is owed while
	// it is off waits for it.
	SetOutgoingWebhookEnabled(ctx context.Context, id uuid.UUID, enabled bool) error

	DeleteOutgoingWebhook(ctx context.Context, id uuid.UUID) error

	// SendOutgoingWebhookTest posts a sample of event, marked as a test, to
	// the webhook now, signed as a real delivery is, and says exactly what was
	// sent and what came back. No event names the webhook's first. It is
	// logged and never retried.
	SendOutgoingWebhookTest(ctx context.Context, id uuid.UUID, event entities.OutgoingWebhookEvent) (entities.OutgoingWebhookTest, error)

	// ListOutgoingWebhookDeliveries reads a page of a webhook's log, newest
	// first.
	ListOutgoingWebhookDeliveries(ctx context.Context, id uuid.UUID, filter entities.OutgoingWebhookLogFilter) ([]entities.OutgoingWebhookLogEntry, error)

	// RetryOutgoingWebhookDelivery makes a pending or failed delivery due now.
	RetryOutgoingWebhookDelivery(ctx context.Context, deliveryID uuid.UUID) error

	// StopOutgoingWebhookDelivery fails a pending delivery, so nothing more is
	// sent of it.
	StopOutgoingWebhookDelivery(ctx context.Context, deliveryID uuid.UUID) error
}

// OutgoingWebhookDeliverer posts what outgoing webhooks are owed.
type OutgoingWebhookDeliverer interface {
	// StartOutgoingWebhookDeliveries starts the worker that posts due
	// deliveries from the database ctx is bound to, until ctx ends.
	StartOutgoingWebhookDeliveries(ctx context.Context)

	// ForgetOldOutgoingDeliveries removes the deliveries past their retention.
	ForgetOldOutgoingDeliveries(ctx context.Context) (int64, error)
}
