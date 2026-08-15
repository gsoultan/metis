package tasks

import (
	"context"
	"encoding/json"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/gobpm/server/endpoints/task"
	"github.com/gsoultan/gobpm/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps task.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/tasks", httptransport.NewServer(
		eps.ListTasks,
		decodeListTasksRequest,
		common.EncodeResponse,
		options...,
	))

	m.Handle("GET /api/v1/tasks/{id}", httptransport.NewServer(
		eps.GetTask,
		decodeGetTaskRequest,
		common.EncodeResponse,
		options...,
	))

	m.Handle("GET /api/v1/tasks/assignee/{assignee}", httptransport.NewServer(
		eps.ListTasksByAssignee,
		decodeListTasksByAssigneeRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/tasks/candidates", httptransport.NewServer(
		eps.ListTasksByCandidates,
		decodeListTasksByCandidatesRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/tasks/{id}/claim", httptransport.NewServer(
		eps.ClaimTask,
		decodeClaimTaskRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/tasks/{id}/unclaim", httptransport.NewServer(
		eps.UnclaimTask,
		decodeUnclaimTaskRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/tasks/{id}/delegate", httptransport.NewServer(
		eps.DelegateTask,
		decodeDelegateTaskRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/tasks/{id}/complete", httptransport.NewServer(
		eps.CompleteTask,
		decodeCompleteTaskRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("PUT /api/v1/tasks/{id}", httptransport.NewServer(
		eps.UpdateTask,
		decodeUpdateTaskRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/tasks/{id}/assign", httptransport.NewServer(
		eps.AssignTask,
		decodeAssignTaskRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeListTasksRequest(_ context.Context, r *http.Request) (any, error) {
	page, pageSize := common.PageParams(r)
	return task.ListTasksRequest{
		ProjectID: r.URL.Query().Get("project_id"),
		Page:      page,
		PageSize:  pageSize,
	}, nil
}

func decodeGetTaskRequest(_ context.Context, r *http.Request) (any, error) {
	id := r.PathValue("id")
	return task.GetTaskRequest{ID: id}, nil
}

func decodeListTasksByAssigneeRequest(_ context.Context, r *http.Request) (any, error) {
	assignee := r.PathValue("assignee")
	return task.ListTasksByAssigneeRequest{Assignee: assignee}, nil
}

func decodeListTasksByCandidatesRequest(_ context.Context, r *http.Request) (any, error) {
	var req task.ListTasksByCandidatesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeClaimTaskRequest(_ context.Context, r *http.Request) (any, error) {
	var req task.ClaimTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	req.ID = r.PathValue("id")
	return req, nil
}

func decodeUnclaimTaskRequest(_ context.Context, r *http.Request) (any, error) {
	return task.UnclaimTaskRequest{ID: r.PathValue("id")}, nil
}

func decodeDelegateTaskRequest(_ context.Context, r *http.Request) (any, error) {
	var req task.DelegateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	req.ID = r.PathValue("id")
	return req, nil
}

func decodeCompleteTaskRequest(_ context.Context, r *http.Request) (any, error) {
	var req task.CompleteTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	id := r.PathValue("id")
	if id != "" {
		req.ID = id
	}
	return req, nil
}

func decodeUpdateTaskRequest(_ context.Context, r *http.Request) (any, error) {
	var req task.UpdateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	req.ID = r.PathValue("id")
	return req, nil
}

func decodeAssignTaskRequest(_ context.Context, r *http.Request) (any, error) {
	var req task.AssignTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	req.ID = r.PathValue("id")
	return req, nil
}
