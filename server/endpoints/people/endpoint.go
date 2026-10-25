// Package people is the API People use: signing in to their project and, from
// there, the work processes give them. Users have their own endpoints and
// cannot reach these, as People cannot reach theirs.
package people

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
	authinterceptor "github.com/gsoultan/metis/server/interceptors/auth"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

type Endpoints struct {
	SignIn             endpoint.Endpoint
	Me                 endpoint.Endpoint
	PersonTasks        endpoint.Endpoint
	PersonTask         endpoint.Endpoint
	ClaimPersonTask    endpoint.Endpoint
	ReleasePersonTask  endpoint.Endpoint
	CompletePersonTask endpoint.Endpoint
	ReadTaskLink       endpoint.Endpoint
	CompleteByTaskLink endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		SignIn:             MakeSignInEndpoint(s),
		Me:                 MakeMeEndpoint(),
		PersonTasks:        MakePersonTasksEndpoint(s),
		PersonTask:         MakePersonTaskEndpoint(s),
		ClaimPersonTask:    MakeClaimPersonTaskEndpoint(s),
		ReleasePersonTask:  MakeReleasePersonTaskEndpoint(s),
		CompletePersonTask: MakeCompletePersonTaskEndpoint(s),
		ReadTaskLink:       MakeReadTaskLinkEndpoint(s),
		CompleteByTaskLink: MakeCompleteByTaskLinkEndpoint(s),
	}
}

// MakeReadTaskLinkEndpoint shows the task a link is for and changes nothing.
func MakeReadTaskLinkEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(TaskLinkRequest)
		if !ok {
			return nil, fmt.Errorf("people: expected a TaskLinkRequest, got %T", request)
		}
		view, err := s.ReadTaskLink(ctx, req.Token)
		if err != nil {
			return TaskLinkResponse{Err: err}, nil
		}
		return TaskLinkResponse{Task: &view.Task, Person: &view.Person, ExpiresAt: view.ExpiresAt}, nil
	}
}

// MakeCompleteByTaskLinkEndpoint completes the task as the link's person and
// uses the link up.
func MakeCompleteByTaskLinkEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(CompleteByTaskLinkRequest)
		if !ok {
			return nil, fmt.Errorf("people: expected a CompleteByTaskLinkRequest, got %T", request)
		}
		if err := s.CompleteByTaskLink(ctx, req.Token, req.Variables); err != nil {
			return PersonTaskActionResponse{Err: err}, nil
		}
		return PersonTaskActionResponse{}, nil
	}
}

func MakeSignInEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(SignInRequest)
		if !ok {
			return nil, fmt.Errorf("people: expected a SignInRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return SignInResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier", req.ProjectID)}, nil
		}
		person, token, expires, err := s.SignInPerson(ctx, projectID, req.Username, req.Password)
		if err != nil {
			return SignInResponse{Err: err}, nil
		}
		return SignInResponse{Person: &person, Token: token, ExpiresAt: expires}, nil
	}
}

// MakeMeEndpoint answers who the token signs in, as the directory has them.
func MakeMeEndpoint() endpoint.Endpoint {
	return func(ctx context.Context, _ any) (any, error) {
		person, ok := authinterceptor.PersonFrom(ctx)
		if !ok {
			return MeResponse{Err: auth.ErrUnauthorized}, nil
		}
		return MeResponse{Person: person}, nil
	}
}

// signedInPerson is the person the token signed in. Every task endpoint acts
// as them and nobody else: there is no field in any request that names who is
// acting.
func signedInPerson(ctx context.Context) (entities.Person, error) {
	person, ok := authinterceptor.PersonFrom(ctx)
	if !ok {
		return entities.Person{}, auth.ErrUnauthorized
	}
	return person, nil
}

func taskID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apierr.Invalidf("task id %q is not a valid identifier", raw)
	}
	return id, nil
}

func MakePersonTasksEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(PersonTasksRequest)
		if !ok {
			return nil, fmt.Errorf("people: expected a PersonTasksRequest, got %T", request)
		}
		person, err := signedInPerson(ctx)
		if err != nil {
			return PersonTasksResponse{Err: err}, nil
		}
		page, err := s.ListPersonTasks(ctx, person, repocontracts.Pagination{Page: req.Page, PageSize: req.PageSize})
		if err != nil {
			return PersonTasksResponse{Err: err}, nil
		}
		tasks := page.Items
		if tasks == nil {
			tasks = []entities.Task{}
		}
		return PersonTasksResponse{
			Tasks: tasks, Total: page.Total, Page: page.Page, PageSize: page.PageSize, HasMore: page.HasMore(),
		}, nil
	}
}

func MakePersonTaskEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(PersonTaskRequest)
		if !ok {
			return nil, fmt.Errorf("people: expected a PersonTaskRequest, got %T", request)
		}
		person, err := signedInPerson(ctx)
		if err != nil {
			return PersonTaskResponse{Err: err}, nil
		}
		id, err := taskID(req.ID)
		if err != nil {
			return PersonTaskResponse{Err: err}, nil
		}
		task, err := s.GetPersonTask(ctx, person, id)
		if err != nil {
			return PersonTaskResponse{Err: err}, nil
		}
		return PersonTaskResponse{Task: &task}, nil
	}
}

func MakeClaimPersonTaskEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return personTaskAction(func(ctx context.Context, person entities.Person, id uuid.UUID, _ any) error {
		return s.ClaimPersonTask(ctx, person, id)
	})
}

func MakeReleasePersonTaskEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return personTaskAction(func(ctx context.Context, person entities.Person, id uuid.UUID, _ any) error {
		return s.ReleasePersonTask(ctx, person, id)
	})
}

func MakeCompletePersonTaskEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(CompletePersonTaskRequest)
		if !ok {
			return nil, fmt.Errorf("people: expected a CompletePersonTaskRequest, got %T", request)
		}
		person, err := signedInPerson(ctx)
		if err != nil {
			return PersonTaskActionResponse{Err: err}, nil
		}
		id, err := taskID(req.ID)
		if err != nil {
			return PersonTaskActionResponse{Err: err}, nil
		}
		return PersonTaskActionResponse{Err: s.CompletePersonTask(ctx, person, id, req.Variables)}, nil
	}
}

// personTaskAction is a claim or a release: a task id from the path, the
// person from the token, and nothing else.
func personTaskAction(act func(ctx context.Context, person entities.Person, id uuid.UUID, request any) error) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(PersonTaskRequest)
		if !ok {
			return nil, fmt.Errorf("people: expected a PersonTaskRequest, got %T", request)
		}
		person, err := signedInPerson(ctx)
		if err != nil {
			return PersonTaskActionResponse{Err: err}, nil
		}
		id, err := taskID(req.ID)
		if err != nil {
			return PersonTaskActionResponse{Err: err}, nil
		}
		return PersonTaskActionResponse{Err: act(ctx, person, id, request)}, nil
	}
}
