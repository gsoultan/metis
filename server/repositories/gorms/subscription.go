package gorms

import (
	"context"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"
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

func (r *subscriptionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return GetTx(ctx, r.db).Delete(&models.Subscription{}, "id = ?", id).Error
}

func (r *subscriptionRepository) DeleteByNode(ctx context.Context, instanceID uuid.UUID, nodeID string) error {
	return GetTx(ctx, r.db).Delete(&models.Subscription{}, "instance_id = ? AND node_id = ?", instanceID, nodeID).Error
}

func (r *subscriptionRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.Subscription, error) {
	var subs []models.Subscription
	err := GetTx(ctx, r.db).Where("instance_id = ?", instanceID).Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return subs, nil
}

func (r *subscriptionRepository) FindSignals(ctx context.Context, projectID uuid.UUID, signalName string) ([]models.Subscription, error) {
	var subs []models.Subscription
	err := GetTx(ctx, r.db).Where("project_id = ? AND type = ? AND event_name = ?", projectID, models.SubscriptionSignal, signalName).Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return subs, nil
}

// ListTemplatedMessageSubscriptions finds message subscriptions still holding an
// unresolved ${...} correlation key. "${" contains no LIKE wildcard, so it is
// matched literally between the two wildcards.
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

func (r *subscriptionRepository) UpdateCorrelationKey(ctx context.Context, id uuid.UUID, correlationKey string) error {
	return GetTx(ctx, r.db).
		Model(&models.Subscription{}).
		Where("id = ?", id).
		Update("correlation_key", correlationKey).Error
}

func (r *subscriptionRepository) FindMessages(ctx context.Context, projectID uuid.UUID, messageName, correlationKey string) ([]models.Subscription, error) {
	var subs []models.Subscription
	query := GetTx(ctx, r.db).Where("project_id = ? AND type = ? AND event_name = ?", projectID, models.SubscriptionMessage, messageName)
	if correlationKey != "" {
		query = query.Where("correlation_key = ?", correlationKey)
	}
	err := query.Find(&subs).Error
	if err != nil {
		return nil, err
	}
	return subs, nil
}
