// Package release is the endpoints that move a project's models from one
// environment to the next: cut a release, import it, make it live, roll back.
package release

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// Endpoints are the release operations.
type Endpoints struct {
	ListReleases    endpoint.Endpoint
	GetRelease      endpoint.Endpoint
	CutRelease      endpoint.Endpoint
	ImportRelease   endpoint.Endpoint
	GoLiveRelease   endpoint.Endpoint
	RollBackRelease endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListReleases: func(ctx context.Context, request any) (any, error) {
			req, ok := request.(ProjectRequest)
			if !ok {
				return nil, fmt.Errorf("release: expected a ProjectRequest, got %T", request)
			}
			projectID, err := parseID("project", req.ProjectID)
			if err != nil {
				return ListResponse{Err: err}, nil
			}
			releases, err := s.ListReleases(ctx, projectID)
			if err != nil {
				return ListResponse{Err: err}, nil
			}
			views := make([]View, 0, len(releases))
			for _, r := range releases {
				views = append(views, ViewOf(r))
			}
			return ListResponse{Releases: views}, nil
		},
		GetRelease: named(func(ctx context.Context, projectID uuid.UUID, name string) (entities.Release, error) {
			return s.GetRelease(ctx, projectID, name)
		}),
		CutRelease: func(ctx context.Context, request any) (any, error) {
			req, ok := request.(CutRequest)
			if !ok {
				return nil, fmt.Errorf("release: expected a CutRequest, got %T", request)
			}
			projectID, err := parseID("project", req.ProjectID)
			if err != nil {
				return ReleaseResponse{Err: err}, nil
			}
			return respond(s.CutRelease(ctx, contracts.CutRelease{
				ProjectID: projectID, Name: req.Name, Processes: req.Processes, Decisions: req.Decisions,
			}))
		},
		ImportRelease: func(ctx context.Context, request any) (any, error) {
			req, ok := request.(ImportRequest)
			if !ok {
				return nil, fmt.Errorf("release: expected an ImportRequest, got %T", request)
			}
			projectID, err := parseID("project", req.ProjectID)
			if err != nil {
				return ReleaseResponse{Err: err}, nil
			}
			source := uuid.Nil
			if req.Environment != "" {
				if source, err = parseID("environment", req.Environment); err != nil {
					return ReleaseResponse{Err: err}, nil
				}
			}
			return respond(s.ImportReleaseFrom(ctx, projectID, source, req.Name))
		},
		GoLiveRelease: named(func(ctx context.Context, projectID uuid.UUID, name string) (entities.Release, error) {
			return s.GoLive(ctx, projectID, name)
		}),
		RollBackRelease: func(ctx context.Context, request any) (any, error) {
			req, ok := request.(ProjectRequest)
			if !ok {
				return nil, fmt.Errorf("release: expected a ProjectRequest, got %T", request)
			}
			projectID, err := parseID("project", req.ProjectID)
			if err != nil {
				return ReleaseResponse{Err: err}, nil
			}
			return respond(s.RollBack(ctx, projectID))
		},
	}
}

func named(fn func(context.Context, uuid.UUID, string) (entities.Release, error)) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(NamedRequest)
		if !ok {
			return nil, fmt.Errorf("release: expected a NamedRequest, got %T", request)
		}
		projectID, err := parseID("project", req.ProjectID)
		if err != nil {
			return ReleaseResponse{Err: err}, nil
		}
		return respond(fn(ctx, projectID, req.Name))
	}
}

func respond(r entities.Release, err error) (any, error) {
	if err != nil {
		return ReleaseResponse{Err: err}, nil
	}
	view := ViewOf(r)
	return ReleaseResponse{Release: &view}, nil
}

func parseID(what, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apierr.Invalidf("%q is not a %s id", raw, what)
	}
	return id, nil
}
