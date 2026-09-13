package processes

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	pbendpoints "github.com/gsoultan/metis/api/proto/endpoints"
	pbentities "github.com/gsoultan/metis/api/proto/entities"
	"github.com/gsoultan/metis/server/endpoints/process"
	"github.com/gsoultan/metis/server/transports/adapters"
	"github.com/gsoultan/metis/server/transports/grpcs/common"
)

type ProcessHandler struct {
	eps process.Endpoints
}

func NewHandler(eps process.Endpoints) *ProcessHandler {
	return &ProcessHandler{eps: eps}
}

func (h *ProcessHandler) StartProcess(ctx context.Context, req *connect.Request[pbendpoints.StartProcessRequest]) (*connect.Response[pbendpoints.StartProcessResponse], error) {
	vars := make(map[string]any)
	if req.Msg.Variables != nil {
		vars = req.Msg.Variables.AsMap()
	}
	response, err := h.eps.StartProcess(ctx, process.StartProcessRequest{
		ProjectID:     req.Msg.ProjectId,
		DefinitionKey: req.Msg.DefinitionKey,
		Variables:     vars,
		Version:       int(req.Msg.Version),
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(process.StartProcessResponse)
	if !ok {
		return nil, fmt.Errorf("processes: expected a process.StartProcessResponse, got %T", response)
	}
	return connect.NewResponse(&pbendpoints.StartProcessResponse{
		InstanceId: resp.InstanceID.String(),
		Error:      common.ErrString(resp.Err),
	}), nil
}

func (h *ProcessHandler) GetInstance(ctx context.Context, req *connect.Request[pbendpoints.GetInstanceRequest]) (*connect.Response[pbendpoints.GetInstanceResponse], error) {
	response, err := h.eps.GetInstance(ctx, process.GetInstanceRequest{
		ID: req.Msg.Id,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(process.GetInstanceResponse)
	if !ok {
		return nil, fmt.Errorf("processes: expected a process.GetInstanceResponse, got %T", response)
	}
	return connect.NewResponse(&pbendpoints.GetInstanceResponse{
		Instance: adapters.ProcessInstancePBAdapter{Instance: resp.Instance}.ToProto(),
		Error:    common.ErrString(resp.Err),
	}), nil
}

func (h *ProcessHandler) ListInstances(ctx context.Context, req *connect.Request[pbendpoints.ListInstancesRequest]) (*connect.Response[pbendpoints.ListInstancesResponse], error) {
	response, err := h.eps.ListInstances(ctx, process.ListInstancesRequest{
		ProjectID:      req.Msg.ProjectId,
		Page:           int(req.Msg.GetPage().GetPage()),
		PageSize:       int(req.Msg.GetPage().GetPageSize()),
		Status:         req.Msg.GetStatus(),
		DefinitionID:   req.Msg.GetDefinitionId(),
		NeedsAttention: req.Msg.GetNeedsAttention(),
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(process.ListInstancesResponse)
	if !ok {
		return nil, fmt.Errorf("processes: expected a process.ListInstancesResponse, got %T", response)
	}
	pbInstances := make([]*pbentities.ProcessInstance, len(resp.Instances))
	for i, inst := range resp.Instances {
		pbInstances[i] = adapters.ProcessInstancePBAdapter{Instance: inst}.ToProto()
	}
	out := &pbendpoints.ListInstancesResponse{
		Instances: pbInstances,
		Error:     common.ErrString(resp.Err),
	}
	if resp.Page != nil {
		out.Page = &pbendpoints.PageInfo{
			Total:    resp.Page.Total,
			Page:     int32(resp.Page.Page),
			PageSize: int32(resp.Page.PageSize),
			HasMore:  resp.Page.HasMore,
		}
	}
	out.StatusCounts = make([]*pbendpoints.StatusCount, len(resp.StatusCounts))
	for i, count := range resp.StatusCounts {
		out.StatusCounts[i] = &pbendpoints.StatusCount{Status: count.Status, Total: count.Total}
	}
	out.NeedsAttentionTotal = resp.NeedsAttentionTotal
	out.NeedsAttentionIds = resp.NeedsAttentionIDs
	return connect.NewResponse(out), nil
}

func (h *ProcessHandler) GetExecutionPath(ctx context.Context, req *connect.Request[pbendpoints.GetExecutionPathRequest]) (*connect.Response[pbendpoints.GetExecutionPathResponse], error) {
	response, err := h.eps.GetExecutionPath(ctx, process.GetExecutionPathRequest{
		InstanceID: req.Msg.InstanceId,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(process.GetExecutionPathResponse)
	if !ok {
		return nil, fmt.Errorf("processes: expected a process.GetExecutionPathResponse, got %T", response)
	}
	freqs := make(map[string]int32)
	for k, v := range resp.Frequencies {
		freqs[k] = int32(v)
	}
	return connect.NewResponse(&pbendpoints.GetExecutionPathResponse{
		Nodes:           adapters.NodesToProto(resp.Nodes),
		NodeFrequencies: freqs,
		Error:           resp.Error,
	}), nil
}
