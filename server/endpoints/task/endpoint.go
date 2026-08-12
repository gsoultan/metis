package task

import (
	"context"
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
		req := request.(GetTaskRequest)
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
		req := request.(ListTasksByAssigneeRequest)

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
		req := request.(ListTasksByCandidatesRequest)

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
		req := request.(ClaimTaskRequest)
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
		req := request.(UnclaimTaskRequest)
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
		req := request.(DelegateTaskRequest)
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
		req := request.(ListTasksRequest)
		var projectID uuid.UUID
		var err error
		if req.ProjectID != "" {
			projectID, err = uuid.Parse(req.ProjectID)
			if err != nil {
				return ListTasksResponse{Err: err}, nil
			}
		}
		tasks, err := s.ListTasks(ctx, projectID)
		return ListTasksResponse{Tasks: tasks, Err: err}, nil
	}
}

func MakeCompleteTaskEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req := request.(CompleteTaskRequest)
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
		req := request.(UpdateTaskRequest)
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
		req := request.(AssignTaskRequest)
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return AssignTaskResponse{Err: err}, nil
		}
		err = s.AssignTask(ctx, id, req.UserID)
		return AssignTaskResponse{Err: err}, nil
	}
}
