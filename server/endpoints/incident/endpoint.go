package incident

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/services"
)

type Endpoints struct {
	ListIncidents   endpoint.Endpoint
	ResolveIncident endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListIncidents:   MakeListIncidentsEndpoint(s),
		ResolveIncident: MakeResolveIncidentEndpoint(s),
	}
}

func MakeListIncidentsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListIncidentsRequest)
		if !ok {
			return nil, fmt.Errorf("incident: expected a ListIncidentsRequest, got %T", request)
		}
		id, err := uuid.Parse(req.InstanceID)
		if err != nil {
			return ListIncidentsResponse{Err: apierr.Invalidf("instance_id %q is not a valid identifier: %v", req.InstanceID, err)}, nil
		}
		incidents, err := s.ListIncidents(ctx, id)
		return ListIncidentsResponse{Incidents: incidents, Err: err}, nil
	}
}

func MakeResolveIncidentEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ResolveIncidentRequest)
		if !ok {
			return nil, fmt.Errorf("incident: expected a ResolveIncidentRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return ResolveIncidentResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		err = s.ResolveIncident(ctx, id)
		return ResolveIncidentResponse{Err: err}, nil
	}
}
