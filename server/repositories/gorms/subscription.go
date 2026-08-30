package gorms

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"gorm.io/gorm"
)

type subscriptionRepository struct {
	db *gorm.DB
}

func NewSubscriptionRepository(db *gorm.DB) contracts.SubscriptionRepository {
	return &subscriptionRepository{db: db}
}

func (r *subscriptionRepository) Create(ctx context.Context, sub models.Subscription) error {
	return GetTx(ctx, r.db).Create(&sub).Error
}

// Delete removes an event subscription, refusing an ID outside the caller's
// tenant.
func (r *subscriptionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	db := GetTx(ctx, r.db)
	if err := requireVisibleToTenant(ctx, db, tableEventSubscriptions, &models.Subscription{}, id); err != nil {
		return err
	}
	return db.Delete(&models.Subscription{}, QualifiedByID(tableEventSubscriptions), id).Error
}

// DeleteByNode removes a node's subscriptions. It takes no row ID, so the tenant
// scope goes into the statement rather than through a guard.
func (r *subscriptionRepository) DeleteByNode(ctx context.Context, instanceID uuid.UUID, nodeID string) error {
	db := tenantScopeCondition(ctx, GetTx(ctx, r.db), tableEventSubscriptions)
	return db.Delete(&models.Subscription{},
		"event_subscriptions.instance_id = ? AND event_subscriptions.node_id = ?", instanceID, nodeID).Error
}

// tableEventSubscriptions is the SQL table behind Subscription, needed by name
// so the tenant scope can build its JOIN.
const tableEventSubscriptions = "event_subscriptions"

// ListByInstance returns an instance's event subscriptions, scoped to the
// caller's tenant.
func (r *subscriptionRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.Subscription, error) {
	var subs []models.Subscription
	err := tenantScopeDB(ctx, GetTx(ctx, r.db), tableEventSubscriptions).
		Where("event_subscriptions.instance_id = ?", instanceID).
		Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return subs, nil
}

// FindSignals resolves a signal to the subscriptions waiting on it. Scoping it
// means a signal raised by one organization can never wake another's process.
func (r *subscriptionRepository) FindSignals(ctx context.Context, projectID uuid.UUID, signalName string) ([]models.Subscription, error) {
	var subs []models.Subscription
	err := tenantScopeDB(ctx, GetTx(ctx, r.db), tableEventSubscriptions).
		Where("event_subscriptions.project_id = ? AND event_subscriptions.type = ? AND event_subscriptions.event_name = ?",
			projectID, models.SubscriptionSignal, signalName).
		Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return subs, nil
}

// ListTemplatedMessageSubscriptions finds message subscriptions still holding an
// unresolved ${...} correlation key. "${" contains no LIKE wildcard, so it is
// matched literally between the two wildcards.
//
// Deliberately not tenant-scoped: this is the background sweep that resolves
// templated keys across the whole installation, and it runs with no request
// context to scope by.
func (r *subscriptionRepository) ListTemplatedMessageSubscriptions(ctx context.Context) ([]models.Subscription, error) {
	var subs []models.Subscription
	err := GetTx(ctx, r.db).
		Where("type = ? AND correlation_key LIKE ?", models.SubscriptionMessage, "%${%").
		Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return subs, nil
}

// UpdateCorrelationKey resolves a templated correlation key, refusing an ID
// outside the caller's tenant. Rewriting another organization's key would
// redirect where its messages correlate.
func (r *subscriptionRepository) UpdateCorrelationKey(ctx context.Context, id uuid.UUID, correlationKey string) error {
	db := GetTx(ctx, r.db)
	if err := requireVisibleToTenant(ctx, db, tableEventSubscriptions, &models.Subscription{}, id); err != nil {
		return err
	}
	return db.
		Model(&models.Subscription{}).
		Where(QualifiedByID(tableEventSubscriptions), id).
		Update("correlation_key", correlationKey).Error
}

// FindMessages resolves a message to the subscriptions waiting on it. Scoping
// it means a message published by one organization can never correlate into
// another's process instance.
func (r *subscriptionRepository) FindMessages(ctx context.Context, projectID uuid.UUID, messageName, correlationKey string) ([]models.Subscription, error) {
	var subs []models.Subscription
	query := tenantScopeDB(ctx, GetTx(ctx, r.db), tableEventSubscriptions).
		Where("event_subscriptions.project_id = ? AND event_subscriptions.type = ? AND event_subscriptions.event_name = ?",
			projectID, models.SubscriptionMessage, messageName)
	if correlationKey != "" {
		query = query.Where("event_subscriptions.correlation_key = ?", correlationKey)
	}
	err := query.Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return subs, nil
}
