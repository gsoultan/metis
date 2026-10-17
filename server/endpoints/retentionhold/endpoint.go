package retentionhold

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/services"
)

// Endpoints read, place and release the holds that keep instances out of
// the finished-instance sweep.
type Endpoints struct {
	GetInstanceHold     endpoint.Endpoint
	HoldInstance        endpoint.Endpoint
	ReleaseInstanceHold endpoint.Endpoint
	ListRetentionHolds  endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		GetInstanceHold:     makeGetInstanceHoldEndpoint(s),
		HoldInstance:        makeHoldInstanceEndpoint(s),
		ReleaseInstanceHold: makeReleaseInstanceHoldEndpoint(s),
		ListRetentionHolds:  makeListRetentionHoldsEndpoint(s),
	}
}

func makeGetInstanceHoldEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(InstanceHoldRequest)
		if !ok {
			return nil, fmt.Errorf("retention hold: expected an InstanceHoldRequest, got %T", request)
		}
		id, err := instanceID(req.InstanceID)
		if err != nil {
			return InstanceHoldResponse{Err: err}, nil
		}
		hold, held, err := s.InstanceHold(ctx, id)
		if err != nil {
			return InstanceHoldResponse{Err: err}, nil
		}
		if !held {
			return InstanceHoldResponse{}, nil
		}
		view := ViewOf(hold)
		return InstanceHoldResponse{Held: true, Hold: &view}, nil
	}
}

func makeHoldInstanceEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ChangeHoldRequest)
		if !ok {
			return nil, fmt.Errorf("retention hold: expected a ChangeHoldRequest, got %T", request)
		}
		id, err := instanceID(req.InstanceID)
		if err != nil {
			return ChangeHoldResponse{Err: err}, nil
		}
		hold, err := s.HoldInstance(ctx, id, req.Reason)
		if err != nil {
			return ChangeHoldResponse{Err: err}, nil
		}
		view := ViewOf(hold)
		return ChangeHoldResponse{Hold: &view}, nil
	}
}

func makeReleaseInstanceHoldEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ChangeHoldRequest)
		if !ok {
			return nil, fmt.Errorf("retention hold: expected a ChangeHoldRequest, got %T", request)
		}
		id, err := instanceID(req.InstanceID)
		if err != nil {
			return ChangeHoldResponse{Err: err}, nil
		}
		hold, err := s.ReleaseInstanceHold(ctx, id, req.Reason)
		if err != nil {
			return ChangeHoldResponse{Err: err}, nil
		}
		view := ViewOf(hold)
		return ChangeHoldResponse{Hold: &view}, nil
	}
}

func makeListRetentionHoldsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListHoldsRequest)
		if !ok {
			return nil, fmt.Errorf("retention hold: expected a ListHoldsRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return ListHoldsResponse{Err: apierr.Invalidf("project id %q is not a valid identifier", req.ProjectID)}, nil
		}
		holds, err := s.ListRetentionHolds(ctx, id)
		if err != nil {
			return ListHoldsResponse{Err: err}, nil
		}
		views := make([]HoldView, 0, len(holds))
		for _, hold := range holds {
			views = append(views, ViewOf(hold))
		}
		return ListHoldsResponse{Holds: views}, nil
	}
}

func instanceID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apierr.Invalidf("instance id %q is not a valid identifier", raw)
	}
	return id, nil
}
