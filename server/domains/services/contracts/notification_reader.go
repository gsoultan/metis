package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// NotificationReader reads one person's notification centre.
type NotificationReader interface {
	// ListByUser returns their newest notifications, no more than a thousand.
	ListByUser(ctx context.Context, userID string) ([]entities.Notification, error)

	// ListByUserPaged returns one page of their notifications, newest first,
	// and how many there are in all — what the notification list reads, a
	// page at a time, rather than being sent a history on every poll.
	ListByUserPaged(ctx context.Context, userID string, page repocontracts.Pagination) (repocontracts.Page[entities.Notification], error)

	// CountUnreadByUser counts every notification of theirs not yet read. It is
	// what the bell shows, and what it polls.
	CountUnreadByUser(ctx context.Context, userID string) (int64, error)
}
