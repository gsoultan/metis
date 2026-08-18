package external_tasks

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	pbendpoints "github.com/gsoultan/gobpm/api/proto/endpoints"
	pbentities "github.com/gsoultan/gobpm/api/proto/entities"
	"github.com/gsoultan/gobpm/server/endpoints/external_task"
	"github.com/gsoultan/gobpm/server/transports/adapters"
)

type ExternalTaskHandler struct {
	eps external_task.Endpoints
}

func NewHandler(eps external_task.Endpoints) *ExternalTaskHandler {
	return &ExternalTaskHandler{eps: eps}
}

func (h *ExternalTaskHandler) FetchAndLockExternalTasks(ctx context.Context, req *connect.Request[pbendpoints.FetchAndLockExternalTasksRequest]) (*connect.Response[pbendpoints.FetchAndLockExternalTasksResponse], error) {
	response, err := h.eps.FetchAndLockExternal(ctx, external_task.FetchAndLockExternalRequest{
		Topic:        req.Msg.Topic,
		WorkerID:     req.Msg.WorkerId,
		MaxTasks:     int(req.Msg.MaxTasks),
		LockDuration: req.Msg.LockDurationMs,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(external_task.FetchAndLockExternalResponse)
	if !ok {
		return nil, fmt.Errorf("external_tasks: expected a external_task.FetchAndLockExternalResponse, got %T", response)
	}
	pbTasks := make([]*pbentities.ExternalTask, len(resp.Tasks))
	for i, t := range resp.Tasks {
		pbTasks[i] = adapters.ExternalTaskPBAdapter{Task: *t}.ToProto()
	}
	return connect.NewResponse(&pbendpoints.FetchAndLockExternalTasksResponse{
		Tasks: pbTasks,
		Error: resp.Error,
	}), nil
}

func (h *ExternalTaskHandler) CompleteExternalTask(ctx context.Context, req *connect.Request[pbendpoints.CompleteExternalTaskRequest]) (*connect.Response[pbendpoints.CompleteExternalTaskResponse], error) {
	id, err := uuid.Parse(req.Msg.TaskId)
	if err != nil {
		//nolint:nilerr // the error is reported in-band, in this API's Error field, not swallowed
		return connect.NewResponse(&pbendpoints.CompleteExternalTaskResponse{Error: err.Error()}), nil
	}
	vars := make(map[string]any)
	if req.Msg.Variables != nil {
		vars = req.Msg.Variables.AsMap()
	}
	response, err := h.eps.CompleteExternal(ctx, external_task.CompleteExternalRequest{
		TaskID:    id,
		WorkerID:  req.Msg.WorkerId,
		Variables: vars,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(external_task.CompleteExternalResponse)
	if !ok {
		return nil, fmt.Errorf("external_tasks: expected a external_task.CompleteExternalResponse, got %T", response)
	}
	return connect.NewResponse(&pbendpoints.CompleteExternalTaskResponse{
		Error: resp.Error,
	}), nil
}

func (h *ExternalTaskHandler) HandleExternalTaskFailure(ctx context.Context, req *connect.Request[pbendpoints.HandleExternalTaskFailureRequest]) (*connect.Response[pbendpoints.HandleExternalTaskFailureResponse], error) {
	id, err := uuid.Parse(req.Msg.TaskId)
	if err != nil {
		//nolint:nilerr // the error is reported in-band, in this API's Error field, not swallowed
		return connect.NewResponse(&pbendpoints.HandleExternalTaskFailureResponse{Error: err.Error()}), nil
	}
	response, err := h.eps.HandleExternalFailure(ctx, external_task.HandleExternalFailureRequest{
		TaskID:       id,
		WorkerID:     req.Msg.WorkerId,
		ErrorMessage: req.Msg.ErrorMessage,
		ErrorDetails: req.Msg.ErrorDetails,
		Retries:      int(req.Msg.Retries),
		RetryTimeout: req.Msg.RetryTimeoutMs,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(external_task.HandleExternalFailureResponse)
	if !ok {
		return nil, fmt.Errorf("external_tasks: expected a external_task.HandleExternalFailureResponse, got %T", response)
	}
	return connect.NewResponse(&pbendpoints.HandleExternalTaskFailureResponse{
		Error: resp.Error,
	}), nil
}
