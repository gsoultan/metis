package contracts

import (
	"context"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

type SubscriptionRepository interface {
	Create(ctx context.Context, sub models.Subscription) error
	Delete(ctx context.Context, id uuid.UUID) error
	DeleteByNode(ctx context.Context, instanceID uuid.UUID, nodeID string) error
	ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.Subscription, error)
	FindSignals(ctx context.Context, projectID uuid.UUID, signalName string) ([]models.Subscription, error)
	FindMessages(ctx context.Context, projectID uuid.UUID, messageName, correlationKey string) ([]models.Subscription, error)

	// ListTemplatedMessageSubscriptions returns message subscriptions whose
	// correlation key is still a raw ${...} template. These are rows written
	// before correlation keys were resolved per instance; they can never match a
	// real inbound correlation value and need a one-off backfill.
	ListTemplatedMessageSubscriptions(ctx context.Context) ([]models.Subscription, error)

	// UpdateCorrelationKey rewrites a single subscription's correlation key.
	UpdateCorrelationKey(ctx context.Context, id uuid.UUID, correlationKey string) error
}
