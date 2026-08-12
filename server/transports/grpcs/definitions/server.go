package definitions

import (
	"context"

	grpctransport "github.com/go-kit/kit/transport/grpc"
	"github.com/gsoultan/gobpm/api/proto/endpoints"
	"github.com/gsoultan/gobpm/api/proto/entities"
	"github.com/gsoultan/gobpm/api/proto/services"
	entities2 "github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/endpoints/definition"
	"github.com/gsoultan/gobpm/server/transports/adapters"
	"github.com/gsoultan/gobpm/server/transports/grpcs/common"

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
	return resp.(*endpoints.CreateDefinitionResponse), nil
}

func (s *Server) ListDefinitions(ctx context.Context, req *endpoints.ListDefinitionsRequest) (*endpoints.ListDefinitionsResponse, error) {
	_, resp, err := s.listDefinitions.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.(*endpoints.ListDefinitionsResponse), nil
}

func (s *Server) GetDefinition(ctx context.Context, req *endpoints.GetDefinitionRequest) (*endpoints.GetDefinitionResponse, error) {
	_, resp, err := s.getDefinition.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.(*endpoints.GetDefinitionResponse), nil
}

func (s *Server) DeleteDefinition(ctx context.Context, req *endpoints.DeleteDefinitionRequest) (*endpoints.DeleteDefinitionResponse, error) {
	_, resp, err := s.deleteDefinition.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.(*endpoints.DeleteDefinitionResponse), nil
}

func decodeGRPCCreateDefinitionRequest(_ context.Context, grpcReq any) (any, error) {
	req := grpcReq.(*endpoints.CreateDefinitionRequest)
	nodes := make([]*entities2.Node, len(req.Nodes))
	for i, n := range req.Nodes {
		nodes[i] = &entities2.Node{
			ID:       n.Id,
			Name:     n.Name,
			Type:     entities2.NodeType(n.Type),
			Assignee: n.Assignee,
			Incoming: n.Incoming,
			Outgoing: n.Outgoing,
		}
	}
	flows := make([]*entities2.SequenceFlow, len(req.Flows))
	for i, f := range req.Flows {
		flows[i] = &entities2.SequenceFlow{
			ID:        f.Id,
			SourceRef: f.SourceRef,
			TargetRef: f.TargetRef,
			Condition: f.Condition,
		}
	}
	projectID, _ := uuid.Parse(req.ProjectId)
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
	resp := response.(definition.CreateDefinitionResponse)
	return &endpoints.CreateDefinitionResponse{Id: resp.ID.String(), Error: common.ErrString(resp.Err)}, nil
}

func decodeGRPCListDefinitionsRequest(_ context.Context, grpcReq any) (any, error) {
	req := grpcReq.(*endpoints.ListDefinitionsRequest)
	return definition.ListDefinitionsRequest{ProjectID: req.ProjectId}, nil
}

func encodeGRPCListDefinitionsResponse(_ context.Context, response any) (any, error) {
	resp := response.(definition.ListDefinitionsResponse)
	var defs []*entities.ProcessDefinition
	if len(resp.Definitions) > 0 {
		defs = make([]*entities.ProcessDefinition, 0, len(resp.Definitions))
		for _, d := range resp.Definitions {
			defs = append(defs, adapters.ProcessDefinitionPBAdapter{Definition: d}.ToProto())
		}
	}
	return &endpoints.ListDefinitionsResponse{Definitions: defs, Error: common.ErrString(resp.Err)}, nil
}

func decodeGRPCGetDefinitionRequest(_ context.Context, grpcReq any) (any, error) {
	req := grpcReq.(*endpoints.GetDefinitionRequest)
	return definition.GetDefinitionRequest{ID: req.Id}, nil
}

func encodeGRPCGetDefinitionResponse(_ context.Context, response any) (any, error) {
	resp := response.(definition.GetDefinitionResponse)
	return &endpoints.GetDefinitionResponse{
		Definition: adapters.ProcessDefinitionPBAdapter{Definition: resp.Definition}.ToProto(),
		Error:      common.ErrString(resp.Err),
	}, nil
}

func decodeGRPCDeleteDefinitionRequest(_ context.Context, grpcReq any) (any, error) {
	req := grpcReq.(*endpoints.DeleteDefinitionRequest)
	return definition.DeleteDefinitionRequest{ID: req.Id}, nil
}

func encodeGRPCDeleteDefinitionResponse(_ context.Context, response any) (any, error) {
	resp := response.(definition.DeleteDefinitionResponse)
	return &endpoints.DeleteDefinitionResponse{Error: common.ErrString(resp.Err)}, nil
}
