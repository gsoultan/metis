package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

type NotificationService interface {
	Send(ctx context.Context, n entities.Notification) error
	ListByUser(ctx context.Context, userID string) ([]entities.Notification, error)
	MarkAsRead(ctx context.Context, id uuid.UUID) error
	MarkAllAsRead(ctx context.Context, userID string) error
	Delete(ctx context.Context, id uuid.UUID) error
}
