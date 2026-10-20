package retentionperiod

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/services"
)

// Endpoints set and read the retention periods processes are given.
type Endpoints struct {
	SetRetentionPeriod     endpoint.Endpoint
	ListRetentionPeriods   endpoint.Endpoint
	RetentionPeriodHistory endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		SetRetentionPeriod:     makeSetRetentionPeriodEndpoint(s),
		ListRetentionPeriods:   makeListRetentionPeriodsEndpoint(s),
		RetentionPeriodHistory: makeRetentionPeriodHistoryEndpoint(s),
	}
}

func makeSetRetentionPeriodEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(SetPeriodRequest)
		if !ok {
			return nil, fmt.Errorf("retention period: expected a SetPeriodRequest, got %T", request)
		}
		id, err := projectID(req.ProjectID)
		if err != nil {
			return SetPeriodResponse{Err: err}, nil
		}
		period, err := s.SetRetentionPeriod(ctx, id, req.ProcessKey, req.Days, req.Reason)
		if err != nil {
			return SetPeriodResponse{Err: err}, nil
		}
		view := ViewOf(period)
		return SetPeriodResponse{Period: &view}, nil
	}
}

func makeListRetentionPeriodsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListPeriodsRequest)
		if !ok {
			return nil, fmt.Errorf("retention period: expected a ListPeriodsRequest, got %T", request)
		}
		id, err := projectID(req.ProjectID)
		if err != nil {
			return ListPeriodsResponse{Err: err}, nil
		}
		periods, err := s.RetentionPeriods(ctx, id)
		if err != nil {
			return ListPeriodsResponse{Err: err}, nil
		}
		return ListPeriodsResponse{Periods: viewsOf(periods)}, nil
	}
}

func makeRetentionPeriodHistoryEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListPeriodsRequest)
		if !ok {
			return nil, fmt.Errorf("retention period: expected a ListPeriodsRequest, got %T", request)
		}
		id, err := projectID(req.ProjectID)
		if err != nil {
			return ListPeriodsResponse{Err: err}, nil
		}
		periods, err := s.RetentionPeriodHistory(ctx, id, req.ProcessKey)
		if err != nil {
			return ListPeriodsResponse{Err: err}, nil
		}
		return ListPeriodsResponse{Periods: viewsOf(periods)}, nil
	}
}

func projectID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apierr.Invalidf("project id %q is not a valid identifier", raw)
	}
	return id, nil
}
