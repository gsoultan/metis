package project

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/services"
)

type Endpoints struct {
	CreateProject endpoint.Endpoint
	GetProject    endpoint.Endpoint
	ListProjects  endpoint.Endpoint
	UpdateProject endpoint.Endpoint
	DeleteProject endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		CreateProject: MakeCreateProjectEndpoint(s),
		GetProject:    MakeGetProjectEndpoint(s),
		ListProjects:  MakeListProjectsEndpoint(s),
		UpdateProject: MakeUpdateProjectEndpoint(s),
		DeleteProject: MakeDeleteProjectEndpoint(s),
	}
}

func MakeCreateProjectEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(CreateProjectRequest)
		if !ok {
			return nil, fmt.Errorf("project: expected a CreateProjectRequest, got %T", request)
		}
		orgID, err := uuid.Parse(req.OrganizationID)
		if err != nil {
			return CreateProjectResponse{Err: apierr.Invalidf("organization_id %q is not a valid identifier: %v", req.OrganizationID, err)}, nil
		}
		p, err := s.CreateProject(ctx, orgID, req.Name, req.Description)
		return CreateProjectResponse{Project: p, Err: err}, nil
	}
}

func MakeGetProjectEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(GetProjectRequest)
		if !ok {
			return nil, fmt.Errorf("project: expected a GetProjectRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return GetProjectResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		p, err := s.GetProject(ctx, id)
		return GetProjectResponse{Project: p, Err: err}, nil
	}
}

func MakeListProjectsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListProjectsRequest)
		if !ok {
			return nil, fmt.Errorf("project: expected a ListProjectsRequest, got %T", request)
		}
		orgID, err := optionalUUID(req.OrganizationID)
		if err != nil {
			return ListProjectsResponse{Err: err}, nil
		}
		projects, err := s.ListProjects(ctx, orgID)
		return ListProjectsResponse{Projects: projects, Err: err}, nil
	}
}

func MakeUpdateProjectEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(UpdateProjectRequest)
		if !ok {
			return nil, fmt.Errorf("project: expected a UpdateProjectRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return UpdateProjectResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		// An organization is optional here — omitting it leaves the project where
		// it is — but a malformed one is a mistake, not an omission.
		orgID, err := optionalUUID(req.OrganizationID)
		if err != nil {
			return UpdateProjectResponse{Err: err}, nil
		}
		err = s.UpdateProject(ctx, id, orgID, req.Name, req.Description)
		return UpdateProjectResponse{Err: err}, nil
	}
}

func MakeDeleteProjectEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DeleteProjectRequest)
		if !ok {
			return nil, fmt.Errorf("project: expected a DeleteProjectRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return DeleteProjectResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		err = s.DeleteProject(ctx, id)
		return DeleteProjectResponse{Err: err}, nil
	}
}

// optionalUUID parses an id that a caller may legitimately omit.
//
// Empty means "not given" and yields uuid.Nil, which the repositories read as
// "do not filter on this". Anything else must be a real id: discarding the parse
// error mapped a typo onto uuid.Nil too, so a malformed organization id quietly
// widened the query to every organization the caller can see instead of being
// refused.
func optionalUUID(raw string) (uuid.UUID, error) {
	if raw == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%q is not a valid id: %w", raw, err)
	}
	return id, nil
}
