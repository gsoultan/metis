package stats

import (
	"context"
	"fmt"

	"github.com/gsoultan/metis/internal/pkg/clamp"

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
		ActiveInstances:    clamp.Int32(resp.ActiveInstances),
		CompletedInstances: clamp.Int32(resp.CompletedInstances),
		FailedInstances:    clamp.Int32(resp.FailedInstances),
		TotalTasks:         clamp.Int32(resp.TotalTasks),
		PendingTasks:       clamp.Int32(resp.PendingTasks),
		CompletedTasks:     clamp.Int32(resp.CompletedTasks),
		Error:              common.ErrString(resp.Err),
	}), nil
}
