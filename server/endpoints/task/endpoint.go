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
		// Releasing puts the task back for anyone to claim, which is handing it
		// over to whoever claims it next. It asked nobody's permission, so any
		// member could release a task somebody else held and take it.
		if err := mayHandOver(ctx, s, id); err != nil {
			return CompleteTaskResponse{Err: err}, nil
		}
		err = s.UnclaimTask(ctx, id)
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
		// parameter — but only the person holding it, or an administrator,
		// may hand it on.
		if err := mayHandOver(ctx, s, id); err != nil {
			return CompleteTaskResponse{Err: err}, nil
		}
		if req.UserID == "" {
			return CompleteTaskResponse{Err: apierr.Invalidf("say who the task is delegated to")}, nil
		}
		err = s.DelegateTask(ctx, id, req.UserID)
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
		task := entities.Task{
			ID:       id,
			Name:     req.Name,
			Priority: req.Priority,
			DueDate:  req.DueDate,
		}
		err = s.UpdateTask(ctx, task)
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
		if err := mayHandOver(ctx, s, id); err != nil {
			return AssignTaskResponse{Err: err}, nil
		}
		if req.UserID == "" {
			return AssignTaskResponse{Err: apierr.Invalidf("say who the task is assigned to")}, nil
		}
		err = s.AssignTask(ctx, id, req.UserID)
		return AssignTaskResponse{Err: err}, nil
	}
}

// callerGroups resolves the candidate groups of the signed-in caller.
//
// Both the group names and IDs are returned because definitions name
// candidate groups either way. An OIDC principal has no local account and so
// no local groups; it matches candidate-user tasks only, which is the honest
// answer until claims are mapped to memberships.
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

// mayHandOver refuses a release, delegate or assign from anyone but the task's
// current assignee or an administrator. A task with no assignee may be
// assigned by an administrator only: absent constraint means deny, not
// "anyone".
func mayHandOver(ctx context.Context, s services.ServiceFacade, id uuid.UUID) error {
	actor, err := principal.Username(ctx)
	if err != nil {
		return err
	}
	if principal.HasRole(ctx, entities.RoleAdmin) {
		return nil
	}
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return err
	}
	if task.AssigneeUsername() == actor {
		return nil
	}
	return apierr.Forbiddenf("only the person holding this task, or an administrator, can hand it to someone else")
}
