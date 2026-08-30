package signals

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
	services.UnimplementedSignalServiceServer
	broadcastSignal grpctransport.Handler
	sendMessage     grpctransport.Handler
}

func NewServer(eps process.Endpoints) *Server {
	return &Server{
		broadcastSignal: grpctransport.NewServer(
			eps.BroadcastSignal,
			decodeGRPCBroadcastSignalRequest,
			encodeGRPCBroadcastSignalResponse,
		),
		sendMessage: grpctransport.NewServer(
			eps.SendMessage,
			decodeGRPCSendMessageRequest,
			encodeGRPCSendMessageResponse,
		),
	}
}

func (s *Server) BroadcastSignal(ctx context.Context, req *endpoints.BroadcastSignalRequest) (*endpoints.BroadcastSignalResponse, error) {
	_, resp, err := s.broadcastSignal.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.BroadcastSignalResponse)
	if !ok {
		return nil, fmt.Errorf("signals: expected a *endpoints.BroadcastSignalResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) SendMessage(ctx context.Context, req *endpoints.SendMessageRequest) (*endpoints.SendMessageResponse, error) {
	_, resp, err := s.sendMessage.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.SendMessageResponse)
	if !ok {
		return nil, fmt.Errorf("signals: expected a *endpoints.SendMessageResponse, got %T", resp)
	}
	return typed, nil
}

func decodeGRPCBroadcastSignalRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.BroadcastSignalRequest)
	if !ok {
		return nil, fmt.Errorf("signals: expected a *endpoints.BroadcastSignalRequest, got %T", grpcReq)
	}
	return process.BroadcastSignalRequest{
		ProjectID:  req.ProjectId,
		SignalName: req.SignalName,
		Variables:  common.DecodeStruct(req.Variables),
	}, nil
}

func encodeGRPCBroadcastSignalResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(process.BroadcastSignalResponse)
	if !ok {
		return nil, fmt.Errorf("signals: expected a process.BroadcastSignalResponse, got %T", response)
	}
	return &endpoints.BroadcastSignalResponse{
		Error: common.ErrString(resp.Err),
	}, nil
}

func decodeGRPCSendMessageRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.SendMessageRequest)
	if !ok {
		return nil, fmt.Errorf("signals: expected a *endpoints.SendMessageRequest, got %T", grpcReq)
	}
	return process.SendMessageRequest{
		ProjectID:      req.ProjectId,
		MessageName:    req.MessageName,
		CorrelationKey: req.CorrelationKey,
		Variables:      common.DecodeStruct(req.Variables),
	}, nil
}

func encodeGRPCSendMessageResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(process.SendMessageResponse)
	if !ok {
		return nil, fmt.Errorf("signals: expected a process.SendMessageResponse, got %T", response)
	}
	return &endpoints.SendMessageResponse{
		Error: common.ErrString(resp.Err),
	}, nil
}
