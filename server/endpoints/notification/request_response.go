package notification

import (
	"github.com/gsoultan/metis/server/domains/entities"
)

type ListNotificationsRequest struct {
	UserID string `json:"user_id"`
}

type ListNotificationsResponse struct {
	Notifications []entities.Notification `json:"notifications"`
	Error         string                  `json:"error,omitzero"`
}

// CountUnreadNotificationsRequest names nobody: the count is the signed-in
// person's.
type CountUnreadNotificationsRequest struct{}

// CountUnreadNotificationsResponse is how many of the signed-in person's
// notifications are unread.
type CountUnreadNotificationsResponse struct {
	UnreadCount int64 `json:"unread_count"`
	Err         error `json:"err,omitzero"`
}

func (r CountUnreadNotificationsResponse) Failed() error { return r.Err }

type MarkAsReadRequest struct {
	ID string `json:"id"`
}

type MarkAsReadResponse struct {
	Error string `json:"error,omitzero"`
}

type MarkAllAsReadRequest struct {
	UserID string `json:"user_id"`
}

type MarkAllAsReadResponse struct {
	Error string `json:"error,omitzero"`
}

type DeleteNotificationRequest struct {
	ID string `json:"id"`
}

type DeleteNotificationResponse struct {
	Error string `json:"error,omitzero"`
}
