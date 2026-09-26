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

// ListOwnNotificationsRequest asks for one page of the signed-in person's
// notifications. Zero means the first page at the server default.
type ListOwnNotificationsRequest struct {
	Page     int `json:"page,omitzero"`
	PageSize int `json:"page_size,omitzero"`
}

// ListOwnNotificationsResponse is one page of the signed-in person's
// notifications, newest first.
type ListOwnNotificationsResponse struct {
	Notifications []entities.Notification `json:"notifications"`
	Page          PageInfo                `json:"page"`
	Err           error                   `json:"err,omitzero"`
}

func (r ListOwnNotificationsResponse) Failed() error { return r.Err }

// PageInfo describes the window returned, so a caller knows whether there is
// an older page to ask for.
type PageInfo struct {
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	HasMore  bool  `json:"has_more"`
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
