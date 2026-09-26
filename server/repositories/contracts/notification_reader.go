package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/repositories/models"
)

// NotificationReader reads one person's notifications, as far as the caller's
// organization may see them.
type NotificationReader interface {
	// ListByUser returns the newest of them, and no more than the store's
	// default of a thousand.
	ListByUser(ctx context.Context, userID string) ([]models.NotificationModel, error)

	// CountUnreadByUser counts every one of them that has not been read,
	// however many there are.
	CountUnreadByUser(ctx context.Context, userID string) (int64, error)
}
