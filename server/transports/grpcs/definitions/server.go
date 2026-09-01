package definitions

import (
	"context"
	"fmt"

	grpctransport "github.com/go-kit/kit/transport/grpc"
	"github.com/gsoultan/metis/api/proto/endpoints"
	"github.com/gsoultan/metis/api/proto/entities"
	"github.com/gsoultan/metis/api/proto/services"
	entities2 "github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/endpoints/definition"
	"github.com/gsoultan/metis/server/transports/adapters"
	"github.com/gsoultan/metis/server/transports/grpcs/common"

	"github.com/google/uuid"
)

type Server struct {
	services.UnimplementedDefinitionServiceServer
	createDefinition grpctransport.Handler
	listDefinitions  grpctransport.Handler
	getDefinition    grpctransport.Handler
	deleteDefinition grpctransport.Handler
}

func NewServer(eps definition.Endpoints) *Server {
	return &Server{
		createDefinition: grpctransport.NewServer(
			eps.CreateDefinition,
			decodeGRPCCreateDefinitionRequest,
			encodeGRPCCreateDefinitionResponse,
		),
		listDefinitions: grpctransport.NewServer(
			eps.ListDefinitions,
			decodeGRPCListDefinitionsRequest,
			encodeGRPCListDefinitionsResponse,
		),
		getDefinition: grpctransport.NewServer(
			eps.GetDefinition,
			decodeGRPCGetDefinitionRequest,
			encodeGRPCGetDefinitionResponse,
		),
		deleteDefinition: grpctransport.NewServer(
			eps.DeleteDefinition,
			decodeGRPCDeleteDefinitionRequest,
			encodeGRPCDeleteDefinitionResponse,
		),
	}
}

func (s *Server) CreateDefinition(ctx context.Context, req *endpoints.CreateDefinitionRequest) (*endpoints.CreateDefinitionResponse, error) {
	_, resp, err := s.createDefinition.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.CreateDefinitionResponse)
	if !ok {
		return nil, fmt.Errorf("definitions: expected a *endpoints.CreateDefinitionResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) ListDefinitions(ctx context.Context, req *endpoints.ListDefinitionsRequest) (*endpoints.ListDefinitionsResponse, error) {
	_, resp, err := s.listDefinitions.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.ListDefinitionsResponse)
	if !ok {
		return nil, fmt.Errorf("definitions: expected a *endpoints.ListDefinitionsResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) GetDefinition(ctx context.Context, req *endpoints.GetDefinitionRequest) (*endpoints.GetDefinitionResponse, error) {
	_, resp, err := s.getDefinition.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.GetDefinitionResponse)
	if !ok {
		return nil, fmt.Errorf("definitions: expected a *endpoints.GetDefinitionResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) DeleteDefinition(ctx context.Context, req *endpoints.DeleteDefinitionRequest) (*endpoints.DeleteDefinitionResponse, error) {
	_, resp, err := s.deleteDefinition.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.DeleteDefinitionResponse)
	if !ok {
		return nil, fmt.Errorf("definitions: expected a *endpoints.DeleteDefinitionResponse, got %T", resp)
	}
	return typed, nil
}

func decodeGRPCCreateDefinitionRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.CreateDefinitionRequest)
	if !ok {
		return nil, fmt.Errorf("definitions: expected a *endpoints.CreateDefinitionRequest, got %T", grpcReq)
	}
	nodes := adapters.NodesFromProto(req.Nodes)
	flows := adapters.FlowsFromProto(req.Flows)
	projectID, err := uuid.Parse(req.ProjectId)
	if err != nil {
		return nil, fmt.Errorf("project id %q is not a valid id: %w", req.ProjectId, err)
	}
	return definition.CreateDefinitionRequest{
		Definition: &entities2.ProcessDefinition{
			Project: &entities2.Project{ID: projectID},
			Key:     req.Key,
			Name:    req.Name,
			Nodes:   nodes,
			Flows:   flows,
		},
	}, nil
}

func encodeGRPCCreateDefinitionResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(definition.CreateDefinitionResponse)
	if !ok {
		return nil, fmt.Errorf("definitions: expected a definition.CreateDefinitionResponse, got %T", response)
	}
	return &endpoints.CreateDefinitionResponse{Id: resp.ID.String(), Error: common.ErrString(resp.Err)}, nil
}

func decodeGRPCListDefinitionsRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.ListDefinitionsRequest)
	if !ok {
		return nil, fmt.Errorf("definitions: expected a *endpoints.ListDefinitionsRequest, got %T", grpcReq)
	}
	return definition.ListDefinitionsRequest{ProjectID: req.ProjectId}, nil
}

func encodeGRPCListDefinitionsResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(definition.ListDefinitionsResponse)
	if !ok {
		return nil, fmt.Errorf("definitions: expected a definition.ListDefinitionsResponse, got %T", response)
	}
	var defs []*entities.ProcessDefinition
	if len(resp.Definitions) > 0 {
		defs = make([]*entities.ProcessDefinition, 0, len(resp.Definitions))
		for _, d := range resp.Definitions {
			defs = append(defs, adapters.ProcessDefinitionPBAdapter{Definition: d}.ToProtoSummary())
		}
	}
	return &endpoints.ListDefinitionsResponse{Definitions: defs, Error: common.ErrString(resp.Err)}, nil
}

func decodeGRPCGetDefinitionRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.GetDefinitionRequest)
	if !ok {
		return nil, fmt.Errorf("definitions: expected a *endpoints.GetDefinitionRequest, got %T", grpcReq)
	}
	return definition.GetDefinitionRequest{ID: req.Id}, nil
}

func encodeGRPCGetDefinitionResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(definition.GetDefinitionResponse)
	if !ok {
		return nil, fmt.Errorf("definitions: expected a definition.GetDefinitionResponse, got %T", response)
	}
	return &endpoints.GetDefinitionResponse{
		Definition: adapters.ProcessDefinitionPBAdapter{Definition: resp.Definition}.ToProto(),
		Error:      common.ErrString(resp.Err),
	}, nil
}

func decodeGRPCDeleteDefinitionRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.DeleteDefinitionRequest)
	if !ok {
		return nil, fmt.Errorf("definitions: expected a *endpoints.DeleteDefinitionRequest, got %T", grpcReq)
	}
	return definition.DeleteDefinitionRequest{ID: req.Id}, nil
}

func encodeGRPCDeleteDefinitionResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(definition.DeleteDefinitionResponse)
	if !ok {
		return nil, fmt.Errorf("definitions: expected a definition.DeleteDefinitionResponse, got %T", response)
	}
	return &endpoints.DeleteDefinitionResponse{Error: common.ErrString(resp.Err)}, nil
}
