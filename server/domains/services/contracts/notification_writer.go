package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// NotificationWriter sends notifications, and records their recipients reading
// and clearing them.
type NotificationWriter interface {
	Send(ctx context.Context, n entities.Notification) error
	MarkAsRead(ctx context.Context, id uuid.UUID) error
	MarkAllAsRead(ctx context.Context, userID string) error
	Delete(ctx context.Context, id uuid.UUID) error
}
