package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/endpoints/task"
	"github.com/gsoultan/metis/server/transports/https/common"
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
	// The literal segment wins over {id}: the mux prefers the more specific
	// pattern, so "delegated" is never read as a task id.
	m.Handle("GET /api/v1/tasks/delegated", httptransport.NewServer(
		eps.ListDelegatedTasks,
		decodeListDelegatedTasksRequest,
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
	m.Handle("POST /api/v1/tasks/{id}/resolve", httptransport.NewServer(
		eps.ResolveTask,
		decodeResolveTaskRequest,
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
		ProjectID:  r.URL.Query().Get("project_id"),
		InstanceID: r.URL.Query().Get("instance_id"),
		Page:       page,
		PageSize:   pageSize,
	}, nil
}

func decodeGetTaskRequest(_ context.Context, r *http.Request) (any, error) {
	id := r.PathValue("id")
	return task.GetTaskRequest{ID: id}, nil
}

func decodeListTasksByAssigneeRequest(_ context.Context, r *http.Request) (any, error) {
	// Same omission the instance listing had: the request declares Page and
	// PageSize and the endpoint hands them to ListTasksByAssigneePaged, but
	// nothing lifted them off the query string — so a user's inbox answered its
	// first page and nothing could ask for the second.
	page, pageSize := common.PageParams(r)
	return task.ListTasksByAssigneeRequest{
		Assignee: r.PathValue("assignee"),
		Page:     page,
		PageSize: pageSize,
	}, nil
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
	var req task.UnclaimTaskRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	req.ID = r.PathValue("id")
	return req, nil
}

func decodeDelegateTaskRequest(_ context.Context, r *http.Request) (any, error) {
	var req task.DelegateTaskRequest
	if err := decodeBody(r, &req); err != nil {
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
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	req.ID = r.PathValue("id")
	return req, nil
}

func decodeAssignTaskRequest(_ context.Context, r *http.Request) (any, error) {
	var req task.AssignTaskRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	req.ID = r.PathValue("id")
	return req, nil
}

func decodeListDelegatedTasksRequest(_ context.Context, r *http.Request) (any, error) {
	page, pageSize := common.PageParams(r)
	return task.ListDelegatedTasksRequest{Page: page, PageSize: pageSize}, nil
}

func decodeResolveTaskRequest(_ context.Context, r *http.Request) (any, error) {
	var req task.ResolveTaskRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	req.ID = r.PathValue("id")
	return req, nil
}

// decodeBody reads a request's JSON body into into.
//
// No body at all is an empty request rather than an error: releasing a task
// never took one, and the Connect and older REST clients send none. A body
// that is there and cannot be read is the caller's mistake and is answered as
// one — it used to reach the encoder as a plain error, which is a 500.
func decodeBody(r *http.Request, into any) error {
	err := json.NewDecoder(r.Body).Decode(into)
	if err == nil || errors.Is(err, io.EOF) {
		return nil
	}
	return apierr.Invalidf("the request body is not JSON the server can read: %v", err)
}
