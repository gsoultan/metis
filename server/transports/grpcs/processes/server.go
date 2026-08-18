package processes

import (
	"context"
	"fmt"

	grpctransport "github.com/go-kit/kit/transport/grpc"
	"github.com/gsoultan/gobpm/api/proto/endpoints"
	"github.com/gsoultan/gobpm/api/proto/entities"
	"github.com/gsoultan/gobpm/api/proto/services"
	"github.com/gsoultan/gobpm/server/endpoints/process"
	"github.com/gsoultan/gobpm/server/transports/adapters"
	"github.com/gsoultan/gobpm/server/transports/grpcs/common"
)

type Server struct {
	services.UnimplementedProcessServiceServer
	startProcess     grpctransport.Handler
	getInstance      grpctransport.Handler
	listInstances    grpctransport.Handler
	getExecutionPath grpctransport.Handler
}

func NewServer(eps process.Endpoints) *Server {
	return &Server{
		startProcess: grpctransport.NewServer(
			eps.StartProcess,
			decodeGRPCStartProcessRequest,
			encodeGRPCStartProcessResponse,
		),
		getInstance: grpctransport.NewServer(
			eps.GetInstance,
			decodeGRPCGetInstanceRequest,
			encodeGRPCGetInstanceResponse,
		),
		listInstances: grpctransport.NewServer(
			eps.ListInstances,
			decodeGRPCListInstancesRequest,
			encodeGRPCListInstancesResponse,
		),
		getExecutionPath: grpctransport.NewServer(
			eps.GetExecutionPath,
			decodeGRPCGetExecutionPathRequest,
			encodeGRPCGetExecutionPathResponse,
		),
	}
}

func (s *Server) StartProcess(ctx context.Context, req *endpoints.StartProcessRequest) (*endpoints.StartProcessResponse, error) {
	_, resp, err := s.startProcess.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.StartProcessResponse)
	if !ok {
		return nil, fmt.Errorf("processes: expected a *endpoints.StartProcessResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) GetInstance(ctx context.Context, req *endpoints.GetInstanceRequest) (*endpoints.GetInstanceResponse, error) {
	_, resp, err := s.getInstance.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.GetInstanceResponse)
	if !ok {
		return nil, fmt.Errorf("processes: expected a *endpoints.GetInstanceResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) ListInstances(ctx context.Context, req *endpoints.ListInstancesRequest) (*endpoints.ListInstancesResponse, error) {
	_, resp, err := s.listInstances.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.ListInstancesResponse)
	if !ok {
		return nil, fmt.Errorf("processes: expected a *endpoints.ListInstancesResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) GetExecutionPath(ctx context.Context, req *endpoints.GetExecutionPathRequest) (*endpoints.GetExecutionPathResponse, error) {
	_, resp, err := s.getExecutionPath.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.GetExecutionPathResponse)
	if !ok {
		return nil, fmt.Errorf("processes: expected a *endpoints.GetExecutionPathResponse, got %T", resp)
	}
	return typed, nil
}

func decodeGRPCStartProcessRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.StartProcessRequest)
	if !ok {
		return nil, fmt.Errorf("processes: expected a *endpoints.StartProcessRequest, got %T", grpcReq)
	}
	vars := make(map[string]any)
	if req.Variables != nil {
		vars = req.Variables.AsMap()
	}
	return process.StartProcessRequest{
		ProjectID:     req.ProjectId,
		DefinitionKey: req.DefinitionKey,
		Variables:     vars,
	}, nil
}

func encodeGRPCStartProcessResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(process.StartProcessResponse)
	if !ok {
		return nil, fmt.Errorf("processes: expected a process.StartProcessResponse, got %T", response)
	}
	return &endpoints.StartProcessResponse{InstanceId: resp.InstanceID.String(), Error: common.ErrString(resp.Err)}, nil
}

func decodeGRPCGetInstanceRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.GetInstanceRequest)
	if !ok {
		return nil, fmt.Errorf("processes: expected a *endpoints.GetInstanceRequest, got %T", grpcReq)
	}
	return process.GetInstanceRequest{ID: req.Id}, nil
}

func encodeGRPCGetInstanceResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(process.GetInstanceResponse)
	if !ok {
		return nil, fmt.Errorf("processes: expected a process.GetInstanceResponse, got %T", response)
	}
	return &endpoints.GetInstanceResponse{
		Instance: adapters.ProcessInstancePBAdapter{Instance: resp.Instance}.ToProto(),
		Error:    common.ErrString(resp.Err),
	}, nil
}

func decodeGRPCListInstancesRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.ListInstancesRequest)
	if !ok {
		return nil, fmt.Errorf("processes: expected a *endpoints.ListInstancesRequest, got %T", grpcReq)
	}
	return process.ListInstancesRequest{ProjectID: req.ProjectId}, nil
}

func encodeGRPCListInstancesResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(process.ListInstancesResponse)
	if !ok {
		return nil, fmt.Errorf("processes: expected a process.ListInstancesResponse, got %T", response)
	}
	var instances []*entities.ProcessInstance
	if len(resp.Instances) > 0 {
		instances = make([]*entities.ProcessInstance, 0, len(resp.Instances))
		for _, inst := range resp.Instances {
			instances = append(instances, adapters.ProcessInstancePBAdapter{Instance: inst}.ToProto())
		}
	}
	return &endpoints.ListInstancesResponse{Instances: instances, Error: common.ErrString(resp.Err)}, nil
}

func decodeGRPCGetExecutionPathRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.GetExecutionPathRequest)
	if !ok {
		return nil, fmt.Errorf("processes: expected a *endpoints.GetExecutionPathRequest, got %T", grpcReq)
	}
	return process.GetExecutionPathRequest{InstanceID: req.InstanceId}, nil
}

func encodeGRPCGetExecutionPathResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(process.GetExecutionPathResponse)
	if !ok {
		return nil, fmt.Errorf("processes: expected a process.GetExecutionPathResponse, got %T", response)
	}
	freqs := make(map[string]int32, len(resp.Frequencies))
	for k, v := range resp.Frequencies {
		freqs[k] = int32(v)
	}
	return &endpoints.GetExecutionPathResponse{
		Nodes:           adapters.NodesToProto(resp.Nodes),
		NodeFrequencies: freqs,
		Error:           resp.Error,
	}, nil
}
