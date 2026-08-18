package task

import (
	"context"
	"fmt"

	repocontracts "github.com/gsoultan/gobpm/server/repositories/contracts"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/domains/services"
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
			return GetTaskResponse{Err: err}, nil
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

func MakeListTasksByCandidatesEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListTasksByCandidatesRequest)
		if !ok {
			return nil, fmt.Errorf("task: expected a ListTasksByCandidatesRequest, got %T", request)
		}

		page, err := s.ListTasksByCandidatesPaged(ctx, req.UserID, req.Groups, repocontracts.Pagination{
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
			return CompleteTaskResponse{Err: err}, nil
		}
		err = s.ClaimTask(ctx, id, req.UserID)
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
			return CompleteTaskResponse{Err: err}, nil
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
		var projectID uuid.UUID
		var err error
		if req.ProjectID != "" {
			projectID, err = uuid.Parse(req.ProjectID)
			if err != nil {
				return ListTasksResponse{Err: err}, nil
			}
		}
		page, err := s.ListTasksPaged(ctx, projectID, repocontracts.Pagination{
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

func MakeCompleteTaskEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(CompleteTaskRequest)
		if !ok {
			return nil, fmt.Errorf("task: expected a CompleteTaskRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return CompleteTaskResponse{Err: err}, nil
		}
		err = s.CompleteTask(ctx, id, req.UserID, req.Variables)
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
			return UpdateTaskResponse{Err: err}, nil
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
			return AssignTaskResponse{Err: err}, nil
		}
		err = s.AssignTask(ctx, id, req.UserID)
		return AssignTaskResponse{Err: err}, nil
	}
}
