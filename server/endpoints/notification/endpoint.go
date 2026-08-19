package notification

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/services"
)

type Endpoints struct {
	ListNotifications  endpoint.Endpoint
	MarkAsRead         endpoint.Endpoint
	MarkAllAsRead      endpoint.Endpoint
	DeleteNotification endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListNotifications:  MakeListNotificationsEndpoint(s),
		MarkAsRead:         MakeMarkAsReadEndpoint(s),
		MarkAllAsRead:      MakeMarkAllAsReadEndpoint(s),
		DeleteNotification: MakeDeleteNotificationEndpoint(s),
	}
}

func MakeListNotificationsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListNotificationsRequest)
		if !ok {
			return nil, fmt.Errorf("notification: expected a ListNotificationsRequest, got %T", request)
		}
		ns, err := s.ListByUser(ctx, req.UserID)
		if err != nil {
			return ListNotificationsResponse{Error: err.Error()}, nil
		}
		return ListNotificationsResponse{Notifications: ns}, nil
	}
}

func MakeMarkAsReadEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(MarkAsReadRequest)
		if !ok {
			return nil, fmt.Errorf("notification: expected a MarkAsReadRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return MarkAsReadResponse{Error: err.Error()}, nil
		}
		err = s.MarkAsRead(ctx, id)
		if err != nil {
			return MarkAsReadResponse{Error: err.Error()}, nil
		}
		return MarkAsReadResponse{}, nil
	}
}

func MakeMarkAllAsReadEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(MarkAllAsReadRequest)
		if !ok {
			return nil, fmt.Errorf("notification: expected a MarkAllAsReadRequest, got %T", request)
		}
		err := s.MarkAllAsRead(ctx, req.UserID)
		if err != nil {
			return MarkAllAsReadResponse{Error: err.Error()}, nil
		}
		return MarkAllAsReadResponse{}, nil
	}
}

func MakeDeleteNotificationEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DeleteNotificationRequest)
		if !ok {
			return nil, fmt.Errorf("notification: expected a DeleteNotificationRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return DeleteNotificationResponse{Error: err.Error()}, nil
		}
		err = s.Delete(ctx, id)
		if err != nil {
			return DeleteNotificationResponse{Error: err.Error()}, nil
		}
		return DeleteNotificationResponse{}, nil
	}
}
