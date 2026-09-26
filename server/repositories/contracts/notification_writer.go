package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

// NotificationWriter records notifications, and their recipients reading and
// clearing them.
type NotificationWriter interface {
	Create(ctx context.Context, n models.NotificationModel) error
	MarkAsRead(ctx context.Context, id uuid.UUID) error
	MarkAllAsRead(ctx context.Context, userID string) error
	Delete(ctx context.Context, id uuid.UUID) error
}
