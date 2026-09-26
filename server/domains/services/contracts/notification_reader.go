package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// NotificationReader reads one person's notification centre.
type NotificationReader interface {
	// ListByUser returns their newest notifications, no more than a thousand.
	ListByUser(ctx context.Context, userID string) ([]entities.Notification, error)

	// CountUnreadByUser counts every notification of theirs not yet read. It is
	// what the bell shows, and what it polls.
	CountUnreadByUser(ctx context.Context, userID string) (int64, error)
}
