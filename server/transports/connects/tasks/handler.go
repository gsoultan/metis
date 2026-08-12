package tasks

import (
	"context"

	"connectrpc.com/connect"
	pbendpoints "github.com/gsoultan/gobpm/api/proto/endpoints"
	pbentities "github.com/gsoultan/gobpm/api/proto/entities"
	"github.com/gsoultan/gobpm/server/endpoints/task"
	"github.com/gsoultan/gobpm/server/transports/adapters"
	"github.com/gsoultan/gobpm/server/transports/grpcs/common"
)

type TaskHandler struct {
	eps task.Endpoints
}

func NewHandler(eps task.Endpoints) *TaskHandler {
	return &TaskHandler{eps: eps}
}

func (h *TaskHandler) GetTask(ctx context.Context, req *connect.Request[pbendpoints.GetTaskRequest]) (*connect.Response[pbendpoints.GetTaskResponse], error) {
	response, err := h.eps.GetTask(ctx, task.GetTaskRequest{
		ID: req.Msg.Id,
	})
	if err != nil {
		return nil, err
	}
	resp := response.(task.GetTaskResponse)
	return connect.NewResponse(&pbendpoints.GetTaskResponse{
		Task:  adapters.TaskPBAdapter{Task: resp.Task}.ToProto(),
		Error: common.ErrString(resp.Err),
	}), nil
}

func (h *TaskHandler) ListTasks(ctx context.Context, req *connect.Request[pbendpoints.ListTasksRequest]) (*connect.Response[pbendpoints.ListTasksResponse], error) {
	response, err := h.eps.ListTasks(ctx, task.ListTasksRequest{
		ProjectID: req.Msg.ProjectId,
	})
	if err != nil {
		return nil, err
	}
	resp := response.(task.ListTasksResponse)
	pbTasks := make([]*pbentities.Task, len(resp.Tasks))
	for i, t := range resp.Tasks {
		pbTasks[i] = adapters.TaskPBAdapter{Task: t}.ToProto()
	}
	return connect.NewResponse(&pbendpoints.ListTasksResponse{
		Tasks: pbTasks,
		Error: common.ErrString(resp.Err),
	}), nil
}

func (h *TaskHandler) ListTasksByAssignee(ctx context.Context, req *connect.Request[pbendpoints.ListTasksByAssigneeRequest]) (*connect.Response[pbendpoints.ListTasksResponse], error) {
	response, err := h.eps.ListTasksByAssignee(ctx, task.ListTasksByAssigneeRequest{
		Assignee: req.Msg.Assignee,
		Page:     int(req.Msg.GetPage().GetPage()),
		PageSize: int(req.Msg.GetPage().GetPageSize()),
	})
	if err != nil {
		return nil, err
	}
	resp := response.(task.ListTasksResponse)
	pbTasks := make([]*pbentities.Task, len(resp.Tasks))
	for i, t := range resp.Tasks {
		pbTasks[i] = adapters.TaskPBAdapter{Task: t}.ToProto()
	}
	out := &pbendpoints.ListTasksResponse{
		Tasks: pbTasks,
		Error: common.ErrString(resp.Err),
	}
	if resp.Page != nil {
		out.Page = &pbendpoints.PageInfo{
			Total:    resp.Page.Total,
			Page:     int32(resp.Page.Page),
			PageSize: int32(resp.Page.PageSize),
			HasMore:  resp.Page.HasMore,
		}
	}
	return connect.NewResponse(out), nil
}

func (h *TaskHandler) ListTasksByCandidates(ctx context.Context, req *connect.Request[pbendpoints.ListTasksByCandidatesRequest]) (*connect.Response[pbendpoints.ListTasksResponse], error) {
	response, err := h.eps.ListTasksByCandidates(ctx, task.ListTasksByCandidatesRequest{
		UserID: req.Msg.UserId,
		Groups: req.Msg.Groups,
	})
	if err != nil {
		return nil, err
	}
	resp := response.(task.ListTasksResponse)
	pbTasks := make([]*pbentities.Task, len(resp.Tasks))
	for i, t := range resp.Tasks {
		pbTasks[i] = adapters.TaskPBAdapter{Task: t}.ToProto()
	}
	return connect.NewResponse(&pbendpoints.ListTasksResponse{
		Tasks: pbTasks,
		Error: common.ErrString(resp.Err),
	}), nil
}

func (h *TaskHandler) ClaimTask(ctx context.Context, req *connect.Request[pbendpoints.ClaimTaskRequest]) (*connect.Response[pbendpoints.ClaimTaskResponse], error) {
	response, err := h.eps.ClaimTask(ctx, task.ClaimTaskRequest{
		ID:     req.Msg.Id,
		UserID: req.Msg.UserId,
	})
	if err != nil {
		return nil, err
	}
	resp := response.(task.CompleteTaskResponse)
	return connect.NewResponse(&pbendpoints.ClaimTaskResponse{
		Error: common.ErrString(resp.Err),
	}), nil
}

func (h *TaskHandler) UnclaimTask(ctx context.Context, req *connect.Request[pbendpoints.UnclaimTaskRequest]) (*connect.Response[pbendpoints.UnclaimTaskResponse], error) {
	response, err := h.eps.UnclaimTask(ctx, task.UnclaimTaskRequest{
		ID: req.Msg.Id,
	})
	if err != nil {
		return nil, err
	}
	resp := response.(task.CompleteTaskResponse)
	return connect.NewResponse(&pbendpoints.UnclaimTaskResponse{
		Error: common.ErrString(resp.Err),
	}), nil
}

func (h *TaskHandler) CompleteTask(ctx context.Context, req *connect.Request[pbendpoints.CompleteTaskRequest]) (*connect.Response[pbendpoints.CompleteTaskResponse], error) {
	vars := make(map[string]any)
	if req.Msg.Variables != nil {
		vars = req.Msg.Variables.AsMap()
	}
	response, err := h.eps.CompleteTask(ctx, task.CompleteTaskRequest{
		ID:        req.Msg.Id,
		UserID:    req.Msg.UserId,
		Variables: vars,
	})
	if err != nil {
		return nil, err
	}
	resp := response.(task.CompleteTaskResponse)
	return connect.NewResponse(&pbendpoints.CompleteTaskResponse{
		Error: common.ErrString(resp.Err),
	}), nil
}
