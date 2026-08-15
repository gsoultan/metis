package gorms

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"gorm.io/gorm"
)

type gormNotificationRepository struct {
	db *gorm.DB
}

func NewNotificationRepository(db *gorm.DB) contracts.NotificationRepository {
	return &gormNotificationRepository{db: db}
}

func (r *gormNotificationRepository) Create(ctx context.Context, n models.NotificationModel) error {
	if err := GetTx(ctx, r.db).Create(&n).Error; err != nil {
		return fmt.Errorf("could not create notification: %w", err)
	}
	return nil
}

// tableNotifications is the SQL table behind NotificationModel, needed by name
// so the tenant scope can build its JOIN.
const tableNotifications = "notifications"

// ListByUser returns a user's notifications, scoped to the caller's tenant.
//
// The scope tolerates a null project_id: a notification raised by a process
// names the project it came from, but a system message to a user names none,
// and an inner join would erase those from the inbox entirely.
func (r *gormNotificationRepository) ListByUser(ctx context.Context, userID string) ([]models.NotificationModel, error) {
	var ms []models.NotificationModel
	db := tenantScopeDBOptionalProject(ctx, GetTx(ctx, r.db), tableNotifications)
	if err := db.Where("notifications.user_id = ?", userID).
		Order("notifications.created_at DESC").
		Find(&ms).Error; err != nil {
		return nil, fmt.Errorf("could not list notifications: %w", err)
	}
	return ms, nil
}

func (r *gormNotificationRepository) MarkAsRead(ctx context.Context, id uuid.UUID) error {
	if err := GetTx(ctx, r.db).Model(&models.NotificationModel{}).Where("id = ?", id).Update("is_read", true).Error; err != nil {
		return fmt.Errorf("could not mark notification as read: %w", err)
	}
	return nil
}

func (r *gormNotificationRepository) MarkAllAsRead(ctx context.Context, userID string) error {
	if err := GetTx(ctx, r.db).Model(&models.NotificationModel{}).Where("user_id = ?", userID).Update("is_read", true).Error; err != nil {
		return fmt.Errorf("could not mark all notifications as read: %w", err)
	}
	return nil
}

func (r *gormNotificationRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if err := GetTx(ctx, r.db).Delete(&models.NotificationModel{}, id).Error; err != nil {
		return fmt.Errorf("could not delete notification: %w", err)
	}
	return nil
}
