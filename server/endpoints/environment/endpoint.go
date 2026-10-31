package environment

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
)

type Endpoints struct {
	ListEnvironments  endpoint.Endpoint
	SaveEnvironment   endpoint.Endpoint
	DeleteEnvironment endpoint.Endpoint
	TestConnection    endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListEnvironments:  MakeListEnvironmentsEndpoint(s),
		SaveEnvironment:   MakeSaveEnvironmentEndpoint(s),
		DeleteEnvironment: MakeDeleteEnvironmentEndpoint(s),
		TestConnection:    MakeTestConnectionEndpoint(s),
	}
}

func MakeListEnvironmentsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListEnvironmentsRequest)
		if !ok {
			return nil, fmt.Errorf("environment: expected a ListEnvironmentsRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return ListEnvironmentsResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		environments, err := s.ListEnvironments(ctx, projectID)
		return ListEnvironmentsResponse{Environments: environments, Err: err}, nil
	}
}

func MakeSaveEnvironmentEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(SaveEnvironmentRequest)
		if !ok {
			return nil, fmt.Errorf("environment: expected a SaveEnvironmentRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return SaveEnvironmentResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		env := entities.Environment{
			Project:    &entities.Project{ID: projectID},
			Name:       req.Name,
			Port:       req.Port,
			Driver:     req.Driver,
			Connection: req.Connection,
			Enabled:    req.Enabled,
		}

		if req.Protected != nil {
			env.Protected = *req.Protected
		}

		if req.ID == "" {
			id, err := s.CreateEnvironment(ctx, env)
			return SaveEnvironmentResponse{ID: id, Err: err}, nil
		}

		id, err := uuid.Parse(req.ID)
		if err != nil {
			return SaveEnvironmentResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		env.ID = id
		if req.Protected == nil {
			stored, err := s.GetEnvironment(ctx, id)
			if err != nil {
				return SaveEnvironmentResponse{Err: err}, nil
			}
			env.Protected = stored.Protected
		}
		return SaveEnvironmentResponse{ID: id, Err: s.UpdateEnvironment(ctx, env)}, nil
	}
}

func MakeDeleteEnvironmentEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DeleteEnvironmentRequest)
		if !ok {
			return nil, fmt.Errorf("environment: expected a DeleteEnvironmentRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return DeleteEnvironmentResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		return DeleteEnvironmentResponse{Err: s.DeleteEnvironment(ctx, id)}, nil
	}
}

// MakeTestConnectionEndpoint reports whether a described database answers.
func MakeTestConnectionEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(TestEnvironmentConnectionRequest)
		if !ok {
			return nil, fmt.Errorf("environment: expected a TestEnvironmentConnectionRequest, got %T", request)
		}
		env := entities.Environment{Driver: req.Driver, Connection: req.Connection}
		if req.ID != "" {
			id, err := uuid.Parse(req.ID)
			if err != nil {
				return TestEnvironmentConnectionResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
			}
			env.ID = id
		}
		health := s.TestEnvironmentConnection(ctx, env)
		return TestEnvironmentConnectionResponse{Reachable: health.Reachable, Detail: health.Detail}, nil
	}
}
