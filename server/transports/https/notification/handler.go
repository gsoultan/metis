package notification

import (
	"context"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/metis/server/endpoints/notification"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps notification.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/notifications", httptransport.NewServer(
		eps.ListNotifications,
		decodeListNotificationsRequest,
		common.EncodeResponse,
		options...,
	))

	m.Handle("POST /api/v1/notifications/{id}/read", httptransport.NewServer(
		eps.MarkAsRead,
		decodeMarkAsReadRequest,
		common.EncodeResponse,
		options...,
	))

	m.Handle("POST /api/v1/notifications/read-all", httptransport.NewServer(
		eps.MarkAllAsRead,
		decodeMarkAllAsReadRequest,
		common.EncodeResponse,
		options...,
	))

	m.Handle("DELETE /api/v1/notifications/{id}", httptransport.NewServer(
		eps.DeleteNotification,
		decodeDeleteNotificationRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeListNotificationsRequest(_ context.Context, r *http.Request) (any, error) {
	return notification.ListNotificationsRequest{
		UserID: r.URL.Query().Get("user_id"),
	}, nil
}

func decodeMarkAsReadRequest(_ context.Context, r *http.Request) (any, error) {
	return notification.MarkAsReadRequest{ID: r.PathValue("id")}, nil
}

func decodeMarkAllAsReadRequest(_ context.Context, r *http.Request) (any, error) {
	return notification.MarkAllAsReadRequest{
		UserID: r.URL.Query().Get("user_id"),
	}, nil
}

func decodeDeleteNotificationRequest(_ context.Context, r *http.Request) (any, error) {
	return notification.DeleteNotificationRequest{ID: r.PathValue("id")}, nil
}
