package tasks

import (
	"context"
	"fmt"

	grpctransport "github.com/go-kit/kit/transport/grpc"
	"github.com/gsoultan/metis/api/proto/endpoints"
	"github.com/gsoultan/metis/api/proto/entities"
	"github.com/gsoultan/metis/api/proto/services"
	"github.com/gsoultan/metis/server/endpoints/task"
	"github.com/gsoultan/metis/server/transports/adapters"
	"github.com/gsoultan/metis/server/transports/grpcs/common"
)

type Server struct {
	services.UnimplementedTaskServiceServer
	getTask               grpctransport.Handler
	listTasks             grpctransport.Handler
	completeTask          grpctransport.Handler
	claimTask             grpctransport.Handler
	unclaimTask           grpctransport.Handler
	listTasksByAssignee   grpctransport.Handler
	listTasksByCandidates grpctransport.Handler
}

func NewServer(eps task.Endpoints) *Server {
	return &Server{
		getTask: grpctransport.NewServer(
			eps.GetTask,
			decodeGRPCGetTaskRequest,
			encodeGRPCGetTaskResponse,
		),
		listTasks: grpctransport.NewServer(
			eps.ListTasks,
			decodeGRPCListTasksRequest,
			encodeGRPCListTasksResponse,
		),
		completeTask: grpctransport.NewServer(
			eps.CompleteTask,
			decodeGRPCCompleteTaskRequest,
			encodeGRPCCompleteTaskResponse,
		),
		claimTask: grpctransport.NewServer(
			eps.ClaimTask,
			decodeGRPCClaimTaskRequest,
			encodeGRPCClaimTaskResponse,
		),
		unclaimTask: grpctransport.NewServer(
			eps.UnclaimTask,
			decodeGRPCUnclaimTaskRequest,
			encodeGRPCUnclaimTaskResponse,
		),
		listTasksByAssignee: grpctransport.NewServer(
			eps.ListTasksByAssignee,
			decodeGRPCListTasksByAssigneeRequest,
			encodeGRPCListTasksResponse,
		),
		listTasksByCandidates: grpctransport.NewServer(
			eps.ListTasksByCandidates,
			decodeGRPCListTasksByCandidatesRequest,
			encodeGRPCListTasksResponse,
		),
	}
}

func (s *Server) GetTask(ctx context.Context, req *endpoints.GetTaskRequest) (*endpoints.GetTaskResponse, error) {
	_, resp, err := s.getTask.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.GetTaskResponse)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a *endpoints.GetTaskResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) ListTasks(ctx context.Context, req *endpoints.ListTasksRequest) (*endpoints.ListTasksResponse, error) {
	_, resp, err := s.listTasks.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.ListTasksResponse)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a *endpoints.ListTasksResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) CompleteTask(ctx context.Context, req *endpoints.CompleteTaskRequest) (*endpoints.CompleteTaskResponse, error) {
	_, resp, err := s.completeTask.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.CompleteTaskResponse)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a *endpoints.CompleteTaskResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) ClaimTask(ctx context.Context, req *endpoints.ClaimTaskRequest) (*endpoints.ClaimTaskResponse, error) {
	_, resp, err := s.claimTask.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.ClaimTaskResponse)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a *endpoints.ClaimTaskResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) UnclaimTask(ctx context.Context, req *endpoints.UnclaimTaskRequest) (*endpoints.UnclaimTaskResponse, error) {
	_, resp, err := s.unclaimTask.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.UnclaimTaskResponse)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a *endpoints.UnclaimTaskResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) ListTasksByAssignee(ctx context.Context, req *endpoints.ListTasksByAssigneeRequest) (*endpoints.ListTasksResponse, error) {
	_, resp, err := s.listTasksByAssignee.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.ListTasksResponse)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a *endpoints.ListTasksResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) ListTasksByCandidates(ctx context.Context, req *endpoints.ListTasksByCandidatesRequest) (*endpoints.ListTasksResponse, error) {
	_, resp, err := s.listTasksByCandidates.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.ListTasksResponse)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a *endpoints.ListTasksResponse, got %T", resp)
	}
	return typed, nil
}

func decodeGRPCGetTaskRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.GetTaskRequest)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a *endpoints.GetTaskRequest, got %T", grpcReq)
	}
	return task.GetTaskRequest{ID: req.Id}, nil
}

func encodeGRPCGetTaskResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(task.GetTaskResponse)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a task.GetTaskResponse, got %T", response)
	}
	return &endpoints.GetTaskResponse{
		Task:  adapters.TaskPBAdapter{Task: resp.Task}.ToProto(),
		Error: common.ErrString(resp.Err),
	}, nil
}

func decodeGRPCListTasksRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.ListTasksRequest)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a *endpoints.ListTasksRequest, got %T", grpcReq)
	}
	return task.ListTasksRequest{ProjectID: req.ProjectId}, nil
}

func encodeGRPCListTasksResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(task.ListTasksResponse)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a task.ListTasksResponse, got %T", response)
	}
	var tasks []*entities.Task
	if len(resp.Tasks) > 0 {
		tasks = make([]*entities.Task, 0, len(resp.Tasks))
		for _, t := range resp.Tasks {
			tasks = append(tasks, adapters.TaskPBAdapter{Task: t}.ToProto())
		}
	}
	return &endpoints.ListTasksResponse{Tasks: tasks, Error: common.ErrString(resp.Err)}, nil
}

func decodeGRPCCompleteTaskRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.CompleteTaskRequest)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a *endpoints.CompleteTaskRequest, got %T", grpcReq)
	}
	vars := make(map[string]any)
	if req.Variables != nil {
		vars = req.Variables.AsMap()
	}
	return task.CompleteTaskRequest{ID: req.Id, Variables: vars}, nil
}

func encodeGRPCCompleteTaskResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(task.CompleteTaskResponse)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a task.CompleteTaskResponse, got %T", response)
	}
	return &endpoints.CompleteTaskResponse{Error: common.ErrString(resp.Err)}, nil
}

func decodeGRPCClaimTaskRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.ClaimTaskRequest)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a *endpoints.ClaimTaskRequest, got %T", grpcReq)
	}
	return task.ClaimTaskRequest{ID: req.Id, UserID: req.UserId}, nil
}

func encodeGRPCClaimTaskResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(task.CompleteTaskResponse)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a task.CompleteTaskResponse, got %T", response)
	}
	return &endpoints.ClaimTaskResponse{Error: common.ErrString(resp.Err)}, nil
}

func decodeGRPCUnclaimTaskRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.UnclaimTaskRequest)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a *endpoints.UnclaimTaskRequest, got %T", grpcReq)
	}
	return task.UnclaimTaskRequest{ID: req.Id}, nil
}

func encodeGRPCUnclaimTaskResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(task.CompleteTaskResponse)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a task.CompleteTaskResponse, got %T", response)
	}
	return &endpoints.UnclaimTaskResponse{Error: common.ErrString(resp.Err)}, nil
}

func decodeGRPCListTasksByAssigneeRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.ListTasksByAssigneeRequest)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a *endpoints.ListTasksByAssigneeRequest, got %T", grpcReq)
	}
	return task.ListTasksByAssigneeRequest{Assignee: req.Assignee}, nil
}

func decodeGRPCListTasksByCandidatesRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.ListTasksByCandidatesRequest)
	if !ok {
		return nil, fmt.Errorf("tasks: expected a *endpoints.ListTasksByCandidatesRequest, got %T", grpcReq)
	}
	return task.ListTasksByCandidatesRequest{UserID: req.UserId, Groups: req.Groups}, nil
}
