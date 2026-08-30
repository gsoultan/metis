package stats

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	pbendpoints "github.com/gsoultan/metis/api/proto/endpoints"
	"github.com/gsoultan/metis/server/endpoints/process"
	"github.com/gsoultan/metis/server/transports/grpcs/common"
)

type StatsHandler struct {
	eps process.Endpoints
}

func NewHandler(eps process.Endpoints) *StatsHandler {
	return &StatsHandler{eps: eps}
}

func (h *StatsHandler) GetProcessStatistics(ctx context.Context, req *connect.Request[pbendpoints.GetProcessStatisticsRequest]) (*connect.Response[pbendpoints.GetProcessStatisticsResponse], error) {
	response, err := h.eps.GetProcessStatistics(ctx, process.GetProcessStatisticsRequest{
		ProjectID: req.Msg.ProjectId,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(process.GetProcessStatisticsResponse)
	if !ok {
		return nil, fmt.Errorf("stats: expected a process.GetProcessStatisticsResponse, got %T", response)
	}
	return connect.NewResponse(&pbendpoints.GetProcessStatisticsResponse{
		ActiveInstances:    int32(resp.ActiveInstances),
		CompletedInstances: int32(resp.CompletedInstances),
		FailedInstances:    int32(resp.FailedInstances),
		TotalTasks:         int32(resp.TotalTasks),
		PendingTasks:       int32(resp.PendingTasks),
		Error:              common.ErrString(resp.Err),
	}), nil
}
