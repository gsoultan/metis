package webhook

import (
	"context"
	"errors"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/domains/services"
)

// errWrongRequestType is returned when the transport hands an endpoint
// something other than its own request type. It cannot happen through the
// handlers in this repository, and asserting without checking would turn a
// future mis-wiring into a panic in a request goroutine.
var errWrongRequestType = errors.New("webhook: the request was not of the expected type")

// Endpoints manage the addresses partners post to. Delivery itself is not here:
// it is public and served straight off the mux, because the signature is over
// the raw body and must not pass through a decoder.
type Endpoints struct {
	ListWebhooks      endpoint.Endpoint
	CreateWebhook     endpoint.Endpoint
	SetWebhookEnabled endpoint.Endpoint
	DeleteWebhook     endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListWebhooks:      MakeListWebhooksEndpoint(s),
		CreateWebhook:     MakeCreateWebhookEndpoint(s),
		SetWebhookEnabled: MakeSetWebhookEnabledEndpoint(s),
		DeleteWebhook:     MakeDeleteWebhookEndpoint(s),
	}
}

func MakeListWebhooksEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListWebhooksRequest)
		if !ok {
			return ListWebhooksResponse{Err: errWrongRequestType}, nil
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return ListWebhooksResponse{Err: err}, nil
		}
		hooks, err := s.ListWebhooks(ctx, projectID)
		return ListWebhooksResponse{Webhooks: hooks, Err: err}, nil
	}
}

func MakeCreateWebhookEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(CreateWebhookRequest)
		if !ok {
			return CreateWebhookResponse{Err: errWrongRequestType}, nil
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return CreateWebhookResponse{Err: err}, nil
		}
		hook, err := s.CreateWebhook(ctx, entities.Webhook{
			Project:               &entities.Project{ID: projectID},
			Name:                  req.Name,
			MessageName:           req.MessageName,
			CorrelationExpression: req.CorrelationExpression,
			SignatureHeader:       req.SignatureHeader,
		})
		return CreateWebhookResponse{Webhook: hook, Err: err}, nil
	}
}

func MakeSetWebhookEnabledEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(SetWebhookEnabledRequest)
		if !ok {
			return SetWebhookEnabledResponse{Err: errWrongRequestType}, nil
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return SetWebhookEnabledResponse{Err: err}, nil
		}
		return SetWebhookEnabledResponse{Err: s.SetWebhookEnabled(ctx, id, req.Enabled)}, nil
	}
}

func MakeDeleteWebhookEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DeleteWebhookRequest)
		if !ok {
			return DeleteWebhookResponse{Err: errWrongRequestType}, nil
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return DeleteWebhookResponse{Err: err}, nil
		}
		return DeleteWebhookResponse{Err: s.DeleteWebhook(ctx, id)}, nil
	}
}
