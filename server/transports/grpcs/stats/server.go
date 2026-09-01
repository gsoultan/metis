package stats

import (
	"context"
	"fmt"

	grpctransport "github.com/go-kit/kit/transport/grpc"
	"github.com/gsoultan/metis/api/proto/endpoints"
	"github.com/gsoultan/metis/api/proto/services"
	"github.com/gsoultan/metis/server/endpoints/process"
	"github.com/gsoultan/metis/server/transports/grpcs/common"
)

type Server struct {
	services.UnimplementedStatsServiceServer
	getStats grpctransport.Handler
}

func NewServer(eps process.Endpoints) *Server {
	return &Server{
		getStats: grpctransport.NewServer(
			eps.GetProcessStatistics,
			decodeGRPCGetStatsRequest,
			encodeGRPCGetStatsResponse,
		),
	}
}

func (s *Server) GetProcessStatistics(ctx context.Context, req *endpoints.GetProcessStatisticsRequest) (*endpoints.GetProcessStatisticsResponse, error) {
	_, resp, err := s.getStats.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.GetProcessStatisticsResponse)
	if !ok {
		return nil, fmt.Errorf("stats: expected a *endpoints.GetProcessStatisticsResponse, got %T", resp)
	}
	return typed, nil
}

func decodeGRPCGetStatsRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.GetProcessStatisticsRequest)
	if !ok {
		return nil, fmt.Errorf("stats: expected a *endpoints.GetProcessStatisticsRequest, got %T", grpcReq)
	}
	return process.GetProcessStatisticsRequest{ProjectID: req.ProjectId}, nil
}

func encodeGRPCGetStatsResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(process.GetProcessStatisticsResponse)
	if !ok {
		return nil, fmt.Errorf("stats: expected a process.GetProcessStatisticsResponse, got %T", response)
	}
	return &endpoints.GetProcessStatisticsResponse{
		ActiveInstances:    int32(resp.ActiveInstances),
		CompletedInstances: int32(resp.CompletedInstances),
		FailedInstances:    int32(resp.FailedInstances),
		TotalTasks:         int32(resp.TotalTasks),
		PendingTasks:       int32(resp.PendingTasks),
		Error:              common.ErrString(resp.Err),
	}, nil
}
