package outgoingwebhook

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

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

	SendOutgoingWebhookTest       endpoint.Endpoint
	ListOutgoingWebhookDeliveries endpoint.Endpoint
	RetryOutgoingWebhookDelivery  endpoint.Endpoint
	StopOutgoingWebhookDelivery   endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListOutgoingWebhooks:      makeListEndpoint(s),
		CreateOutgoingWebhook:     makeCreateEndpoint(s),
		SetOutgoingWebhookEnabled: makeSetEnabledEndpoint(s),
		DeleteOutgoingWebhook:     makeDeleteEndpoint(s),

		SendOutgoingWebhookTest:       makeSendTestEndpoint(s),
		ListOutgoingWebhookDeliveries: makeListDeliveriesEndpoint(s),
		RetryOutgoingWebhookDelivery: makeDeliveryEndpoint(func(ctx context.Context, id uuid.UUID) error {
			return s.RetryOutgoingWebhookDelivery(ctx, id)
		}),
		StopOutgoingWebhookDelivery: makeDeliveryEndpoint(func(ctx context.Context, id uuid.UUID) error {
			return s.StopOutgoingWebhookDelivery(ctx, id)
		}),
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

func makeSendTestEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(SendTestRequest)
		if !ok {
			return SendTestResponse{Err: errWrongRequestType}, nil
		}
		id, err := parseID("outgoing webhook", req.ID)
		if err != nil {
			return SendTestResponse{Err: err}, nil
		}
		test, err := s.SendOutgoingWebhookTest(ctx, id, entities.OutgoingWebhookEvent(req.Event))
		if err != nil {
			return SendTestResponse{Err: err}, nil
		}
		view := TestViewOf(test)
		return SendTestResponse{Test: &view}, nil
	}
}

func makeListDeliveriesEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListDeliveriesRequest)
		if !ok {
			return ListDeliveriesResponse{Err: errWrongRequestType}, nil
		}
		id, err := parseID("outgoing webhook", req.ID)
		if err != nil {
			return ListDeliveriesResponse{Err: err}, nil
		}
		limit := logPage(req.Limit)
		filter := entities.OutgoingWebhookLogFilter{
			Status: entities.OutgoingWebhookDeliveryStatus(req.Status),
			Limit:  limit,
		}
		if req.Before != "" {
			before, err := decodeCursor(req.Before)
			if err != nil {
				return ListDeliveriesResponse{Err: err}, nil
			}
			filter.Before = &before
		}
		entries, err := s.ListOutgoingWebhookDeliveries(ctx, id, filter)
		if err != nil {
			return ListDeliveriesResponse{Err: err}, nil
		}
		views := make([]DeliveryView, 0, len(entries))
		for _, entry := range entries {
			views = append(views, DeliveryViewOf(entry))
		}
		resp := ListDeliveriesResponse{Deliveries: views}
		// A full page may have more behind it; a short one has not.
		if len(entries) == limit {
			last := entries[len(entries)-1]
			resp.NextBefore = encodeCursor(entities.OutgoingWebhookLogCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		}
		return resp, nil
	}
}

func makeDeliveryEndpoint(act func(context.Context, uuid.UUID) error) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DeliveryRequest)
		if !ok {
			return DeliveryResponse{Err: errWrongRequestType}, nil
		}
		id, err := parseID("delivery", req.DeliveryID)
		if err != nil {
			return DeliveryResponse{Err: err}, nil
		}
		return DeliveryResponse{Err: act(ctx, id)}, nil
	}
}

// defaultLogPage and maxLogPage bound a page of the log.
const (
	defaultLogPage = 50
	maxLogPage     = 200
)

func logPage(asked int) int {
	switch {
	case asked <= 0:
		return defaultLogPage
	case asked > maxLogPage:
		return maxLogPage
	default:
		return asked
	}
}

// encodeCursor and decodeCursor carry where a page ended as one opaque token:
// the delivery's creation time to the nanosecond and its id.
func encodeCursor(c entities.OutgoingWebhookLogCursor) string {
	raw := strconv.FormatInt(c.CreatedAt.UnixNano(), 10) + "." + c.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(token string) (entities.OutgoingWebhookLogCursor, error) {
	invalid := apierr.Invalidf("%q is not a page of this log; pass the next_before of the previous page", token)
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return entities.OutgoingWebhookLogCursor{}, invalid
	}
	nanos, id, found := strings.Cut(string(raw), ".")
	if !found {
		return entities.OutgoingWebhookLogCursor{}, invalid
	}
	at, err := strconv.ParseInt(nanos, 10, 64)
	if err != nil {
		return entities.OutgoingWebhookLogCursor{}, invalid
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return entities.OutgoingWebhookLogCursor{}, invalid
	}
	return entities.OutgoingWebhookLogCursor{CreatedAt: time.Unix(0, at).UTC(), ID: parsed}, nil
}

func parseID(what, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apierr.Invalidf("%q is not a valid %s id", raw, what)
	}
	return id, nil
}
