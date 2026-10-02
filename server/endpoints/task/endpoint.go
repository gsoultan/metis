package task

import (
	"context"
	"fmt"

	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/endpoints/principal"
)

type Endpoints struct {
	GetTask               endpoint.Endpoint
	ListTasks             endpoint.Endpoint
	ListTasksByAssignee   endpoint.Endpoint
	ListTasksByCandidates endpoint.Endpoint
	ClaimTask             endpoint.Endpoint
	UnclaimTask           endpoint.Endpoint
	DelegateTask          endpoint.Endpoint
	CompleteTask          endpoint.Endpoint
	UpdateTask            endpoint.Endpoint
	AssignTask            endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		GetTask:               MakeGetTaskEndpoint(s),
		ListTasks:             MakeListTasksEndpoint(s),
		ListTasksByAssignee:   MakeListTasksByAssigneeEndpoint(s),
		ListTasksByCandidates: MakeListTasksByCandidatesEndpoint(s),
		ClaimTask:             MakeClaimTaskEndpoint(s),
		UnclaimTask:           MakeUnclaimTaskEndpoint(s),
		DelegateTask:          MakeDelegateTaskEndpoint(s),
		CompleteTask:          MakeCompleteTaskEndpoint(s),
		UpdateTask:            MakeUpdateTaskEndpoint(s),
		AssignTask:            MakeAssignTaskEndpoint(s),
	}
}

func MakeGetTaskEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(GetTaskRequest)
		if !ok {
			return nil, fmt.Errorf("task: expected a GetTaskRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return GetTaskResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		task, err := s.GetTask(ctx, id)
		return GetTaskResponse{Task: task, Err: err}, nil
	}
}

func MakeListTasksByAssigneeEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListTasksByAssigneeRequest)
		if !ok {
			return nil, fmt.Errorf("task: expected a ListTasksByAssigneeRequest, got %T", request)
		}

		page, err := s.ListTasksByAssigneePaged(ctx, req.Assignee, repocontracts.Pagination{
			Page:     req.Page,
			PageSize: req.PageSize,
		})
		if err != nil {
			return ListTasksResponse{Err: err}, nil
		}
		return listTasksResponse(page), nil
	}
}

// listTasksResponse shapes a page of tasks for the wire. Both branches of the
// listing answer in the same shape, and writing it twice is how they drift.
func listTasksResponse(page repocontracts.Page[entities.Task]) ListTasksResponse {
	return ListTasksResponse{
		Tasks: page.Items,
		Page: &PageInfo{
			Total:    page.Total,
			Page:     page.Page,
			PageSize: page.PageSize,
			HasMore:  page.HasMore(),
		},
	}
}

func MakeListTasksByCandidatesEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListTasksByCandidatesRequest)
		if !ok {
			return nil, fmt.Errorf("task: expected a ListTasksByCandidatesRequest, got %T", request)
		}

		// The inbox is the caller's own. Whatever user_id or groups the
		// request names are ignored: the actor is the verified principal and
		// the candidate groups are the memberships the server knows about,
		// not a list the browser asserts.
		actor, err := principal.Username(ctx)
		if err != nil {
			return ListTasksResponse{Err: err}, nil
		}
		groups, err := callerGroups(ctx, s)
		if err != nil {
			return ListTasksResponse{Err: err}, nil
		}
		page, err := s.ListTasksByCandidatesPaged(ctx, actor, groups, repocontracts.Pagination{
			Page:     req.Page,
			PageSize: req.PageSize,
		})
		if err != nil {
			return ListTasksResponse{Err: err}, nil
		}
		return ListTasksResponse{
			Tasks: page.Items,
			Page: &PageInfo{
				Total:    page.Total,
				Page:     page.Page,
				PageSize: page.PageSize,
				HasMore:  page.HasMore(),
			},
		}, nil
	}
}

func MakeClaimTaskEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ClaimTaskRequest)
		if !ok {
			return nil, fmt.Errorf("task: expected a ClaimTaskRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return CompleteTaskResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		actor, err := principal.Username(ctx)
		if err != nil {
			return CompleteTaskResponse{Err: err}, nil
		}
		err = s.ClaimTask(ctx, id, actor)
		return CompleteTaskResponse{Err: err}, nil
	}
}

func MakeUnclaimTaskEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(UnclaimTaskRequest)
		if !ok {
			return nil, fmt.Errorf("task: expected a UnclaimTaskRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return CompleteTaskResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		actor, err := principal.Username(ctx)
		if err != nil {
			return CompleteTaskResponse{Err: err}, nil
		}
		// Who may release it, and whether they must say why, is the service's
		// to decide: it holds the task's row while it does.
		err = s.UnclaimTask(ctx, id, servicecontracts.HandOver{Actor: actor, Reason: req.Reason})
		return CompleteTaskResponse{Err: err}, nil
	}
}

func MakeDelegateTaskEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DelegateTaskRequest)
		if !ok {
			return nil, fmt.Errorf("task: expected a DelegateTaskRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return CompleteTaskResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		// Delegation hands the task to someone else, so the target is a
		// parameter — the caller is not: it is whoever the token says.
		actor, err := principal.Username(ctx)
		if err != nil {
			return CompleteTaskResponse{Err: err}, nil
		}
		err = s.DelegateTask(ctx, id, servicecontracts.HandOver{Actor: actor, Target: req.UserID, Reason: req.Reason})
		return CompleteTaskResponse{Err: err}, nil
	}
}

func MakeListTasksEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListTasksRequest)
		if !ok {
			return nil, fmt.Errorf("task: expected a ListTasksRequest, got %T", request)
		}
		pagination := repocontracts.Pagination{Page: req.Page, PageSize: req.PageSize}

		// An instance narrows harder than a project and already implies one, so
		// it wins outright rather than combining. A malformed id is refused
		// rather than ignored: silently widening to the whole project would
		// answer a different question than the one asked.
		if req.InstanceID != "" {
			instanceID, err := uuid.Parse(req.InstanceID)
			if err != nil {
				return ListTasksResponse{Err: apierr.Invalidf("instance_id %q is not a valid identifier: %v", req.InstanceID, err)}, nil
			}
			page, err := s.ListTasksByInstancePaged(ctx, instanceID, pagination)
			if err != nil {
				return ListTasksResponse{Err: err}, nil
			}
			return listTasksResponse(page), nil
		}

		var projectID uuid.UUID
		var err error
		if req.ProjectID != "" {
			projectID, err = uuid.Parse(req.ProjectID)
			if err != nil {
				return ListTasksResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
			}
		}
		page, err := s.ListTasksPaged(ctx, projectID, pagination)
		if err != nil {
			return ListTasksResponse{Err: err}, nil
		}
		return ListTasksResponse{
			Tasks: page.Items,
			Page: &PageInfo{
				Total:    page.Total,
				Page:     page.Page,
				PageSize: page.PageSize,
				HasMore:  page.HasMore(),
			},
		}, nil
	}
}

func MakeCompleteTaskEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(CompleteTaskRequest)
		if !ok {
			return nil, fmt.Errorf("task: expected a CompleteTaskRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return CompleteTaskResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		actor, err := principal.Username(ctx)
		if err != nil {
			return CompleteTaskResponse{Err: err}, nil
		}
		err = s.CompleteTask(ctx, id, actor, req.Variables)
		return CompleteTaskResponse{Err: err}, nil
	}
}

func MakeUpdateTaskEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(UpdateTaskRequest)
		if !ok {
			return nil, fmt.Errorf("task: expected a UpdateTaskRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return UpdateTaskResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		actor, err := principal.Username(ctx)
		if err != nil {
			return UpdateTaskResponse{Err: err}, nil
		}
		// A task's name, priority and due date are how its holder orders their
		// day. Who may change them, and whether they must say why, is decided
		// by the service on the row it holds.
		err = s.UpdateTask(ctx, id, servicecontracts.TaskEdit{
			Actor:        actor,
			Reason:       req.Reason,
			Name:         req.Name,
			Priority:     req.Priority,
			DueDate:      req.DueDate.Value,
			ClearDueDate: req.DueDate.Present && req.DueDate.Value == nil,
		})
		return UpdateTaskResponse{Err: err}, nil
	}
}

func MakeAssignTaskEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(AssignTaskRequest)
		if !ok {
			return nil, fmt.Errorf("task: expected a AssignTaskRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return AssignTaskResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		actor, err := principal.Username(ctx)
		if err != nil {
			return AssignTaskResponse{Err: err}, nil
		}
		err = s.AssignTask(ctx, id, servicecontracts.HandOver{Actor: actor, Target: req.UserID, Reason: req.Reason})
		return AssignTaskResponse{Err: err}, nil
	}
}

// callerGroups resolves the candidate groups of the signed-in caller.
//
// Both the group names and IDs are returned because definitions name
// candidate groups either way. Somebody signed in through an identity provider
// is in the groups an administrator put their linked account in, as a local
// account is.
func callerGroups(ctx context.Context, s services.ServiceFacade) ([]string, error) {
	user, ok := principal.LocalUser(ctx)
	if !ok || user.ID == uuid.Nil {
		return nil, nil
	}
	groups, err := s.ListUserGroups(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("resolve the caller's groups: %w", err)
	}
	out := make([]string, 0, 2*len(groups))
	for _, g := range groups {
		if g.Name != "" {
			out = append(out, g.Name)
		}
		if g.ID != uuid.Nil {
			out = append(out, g.ID.String())
		}
	}
	return out, nil
}
