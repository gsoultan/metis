package deviation

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/services"
)

type Endpoints struct {
	ListInstanceDeviations endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListInstanceDeviations: MakeListInstanceDeviationsEndpoint(s),
	}
}

func MakeListInstanceDeviationsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListInstanceDeviationsRequest)
		if !ok {
			return nil, fmt.Errorf("deviation: expected a ListInstanceDeviationsRequest, got %T", request)
		}
		id, err := uuid.Parse(req.InstanceID)
		if err != nil {
			return ListInstanceDeviationsResponse{Err: apierr.Invalidf("instance id %q is not a valid identifier", req.InstanceID)}, nil
		}
		rows, err := s.ListInstanceDeviations(ctx, id)
		if err != nil {
			return ListInstanceDeviationsResponse{Err: err}, nil
		}
		views := make([]DeviationView, 0, len(rows))
		for _, row := range rows {
			views = append(views, ViewOf(row))
		}
		return ListInstanceDeviationsResponse{Deviations: views}, nil
	}
}
