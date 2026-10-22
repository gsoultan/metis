package outgoingwebhook

import (
	"context"
	"errors"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
)

// errWrongRequestType is returned when the transport hands an endpoint
// something other than its own request type, which only a mis-wiring can do.
var errWrongRequestType = errors.New("outgoing webhook: the request was not of the expected type")

// Endpoints manage the addresses a project posts its events to.
type Endpoints struct {
	ListOutgoingWebhooks      endpoint.Endpoint
	CreateOutgoingWebhook     endpoint.Endpoint
	SetOutgoingWebhookEnabled endpoint.Endpoint
	DeleteOutgoingWebhook     endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListOutgoingWebhooks:      makeListEndpoint(s),
		CreateOutgoingWebhook:     makeCreateEndpoint(s),
		SetOutgoingWebhookEnabled: makeSetEnabledEndpoint(s),
		DeleteOutgoingWebhook:     makeDeleteEndpoint(s),
	}
}

func makeListEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListRequest)
		if !ok {
			return ListResponse{Err: errWrongRequestType}, nil
		}
		projectID, err := parseID("project", req.ProjectID)
		if err != nil {
			return ListResponse{Err: err}, nil
		}
		hooks, err := s.ListOutgoingWebhooks(ctx, projectID)
		if err != nil {
			return ListResponse{Err: err}, nil
		}
		views := make([]WebhookView, 0, len(hooks))
		for _, hook := range hooks {
			views = append(views, ViewOf(hook))
		}
		return ListResponse{Webhooks: views}, nil
	}
}

func makeCreateEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(CreateRequest)
		if !ok {
			return CreateResponse{Err: errWrongRequestType}, nil
		}
		projectID, err := parseID("project", req.ProjectID)
		if err != nil {
			return CreateResponse{Err: err}, nil
		}
		events := make([]entities.OutgoingWebhookEvent, 0, len(req.Events))
		for _, event := range req.Events {
			events = append(events, entities.OutgoingWebhookEvent(event))
		}
		hook, err := s.CreateOutgoingWebhook(ctx, entities.OutgoingWebhook{
			Project: &entities.Project{ID: projectID},
			Name:    req.Name,
			URL:     req.URL,
			Events:  events,
		})
		if err != nil {
			return CreateResponse{Err: err}, nil
		}
		view := ViewOf(hook)
		return CreateResponse{Webhook: &view}, nil
	}
}

func makeSetEnabledEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(SetEnabledRequest)
		if !ok {
			return SetEnabledResponse{Err: errWrongRequestType}, nil
		}
		id, err := parseID("outgoing webhook", req.ID)
		if err != nil {
			return SetEnabledResponse{Err: err}, nil
		}
		return SetEnabledResponse{Err: s.SetOutgoingWebhookEnabled(ctx, id, req.Enabled)}, nil
	}
}

func makeDeleteEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DeleteRequest)
		if !ok {
			return DeleteResponse{Err: errWrongRequestType}, nil
		}
		id, err := parseID("outgoing webhook", req.ID)
		if err != nil {
			return DeleteResponse{Err: err}, nil
		}
		return DeleteResponse{Err: s.DeleteOutgoingWebhook(ctx, id)}, nil
	}
}

func parseID(what, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apierr.Invalidf("%q is not a valid %s id", raw, what)
	}
	return id, nil
}
