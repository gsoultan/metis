package notification

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/endpoints/principal"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

type Endpoints struct {
	ListNotifications        endpoint.Endpoint
	ListOwnNotifications     endpoint.Endpoint
	CountUnreadNotifications endpoint.Endpoint
	MarkAsRead               endpoint.Endpoint
	MarkAllAsRead            endpoint.Endpoint
	DeleteNotification       endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListNotifications:        MakeListNotificationsEndpoint(s),
		ListOwnNotifications:     MakeListOwnNotificationsEndpoint(s),
		CountUnreadNotifications: MakeCountUnreadNotificationsEndpoint(s),
		MarkAsRead:               MakeMarkAsReadEndpoint(s),
		MarkAllAsRead:            MakeMarkAllAsReadEndpoint(s),
		DeleteNotification:       MakeDeleteNotificationEndpoint(s),
	}
}

// MakeListOwnNotificationsEndpoint returns one page of the signed-in person's
// notifications, newest first, and where it sits among all of them.
//
// Whose they are comes from the session, as for the count below. Zero paging
// is the first page at the server default, so a caller that asks for nothing
// still gets a bounded answer.
func MakeListOwnNotificationsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListOwnNotificationsRequest)
		if !ok {
			return nil, fmt.Errorf("notification: expected a ListOwnNotificationsRequest, got %T", request)
		}
		recipient, err := principal.Username(ctx)
		if err != nil {
			return ListOwnNotificationsResponse{Err: err}, nil
		}
		page, err := s.ListByUserPaged(ctx, recipient, repocontracts.Pagination{Page: req.Page, PageSize: req.PageSize})
		if err != nil {
			return ListOwnNotificationsResponse{Err: err}, nil
		}
		return ListOwnNotificationsResponse{
			Notifications: page.Items,
			Page: PageInfo{
				Total:    page.Total,
				Page:     page.Page,
				PageSize: page.PageSize,
				HasMore:  page.HasMore(),
			},
		}, nil
	}
}

// MakeCountUnreadNotificationsEndpoint answers how many of the signed-in
// person's notifications are unread: the bell's number, and what it polls.
//
// Whose they are comes from the session and from nothing in the request.
// ListNotifications takes its recipient from a user_id on the wire; this takes
// it from where a request cannot choose it. See principal for why the actor is
// not a parameter.
func MakeCountUnreadNotificationsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		if _, ok := request.(CountUnreadNotificationsRequest); !ok {
			return nil, fmt.Errorf("notification: expected a CountUnreadNotificationsRequest, got %T", request)
		}
		recipient, err := principal.Username(ctx)
		if err != nil {
			return CountUnreadNotificationsResponse{Err: err}, nil
		}
		unread, err := s.CountUnreadByUser(ctx, recipient)
		if err != nil {
			return CountUnreadNotificationsResponse{Err: err}, nil
		}
		return CountUnreadNotificationsResponse{UnreadCount: unread}, nil
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
