package definitions

import (
	"context"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	pbendpoints "github.com/gsoultan/gobpm/api/proto/endpoints"
	pbentities "github.com/gsoultan/gobpm/api/proto/entities"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/endpoints/definition"
	"github.com/gsoultan/gobpm/server/transports/adapters"
	"github.com/gsoultan/gobpm/server/transports/grpcs/common"
)

type DefinitionHandler struct {
	eps definition.Endpoints
}

func NewHandler(eps definition.Endpoints) *DefinitionHandler {
	return &DefinitionHandler{eps: eps}
}

func (h *DefinitionHandler) CreateDefinition(ctx context.Context, req *connect.Request[pbendpoints.CreateDefinitionRequest]) (*connect.Response[pbendpoints.CreateDefinitionResponse], error) {
	nodes := make([]*entities.Node, len(req.Msg.Nodes))
	for i, n := range req.Msg.Nodes {
		nodes[i] = &entities.Node{
			ID:       n.Id,
			Name:     n.Name,
			Type:     entities.NodeType(n.Type),
			Assignee: n.Assignee,
			Incoming: n.Incoming,
			Outgoing: n.Outgoing,
		}
	}
	flows := make([]*entities.SequenceFlow, len(req.Msg.Flows))
	for i, f := range req.Msg.Flows {
		flows[i] = &entities.SequenceFlow{
			ID:        f.Id,
			SourceRef: f.SourceRef,
			TargetRef: f.TargetRef,
			Condition: f.Condition,
		}
	}
	projectID, _ := uuid.Parse(req.Msg.ProjectId)
	response, err := h.eps.CreateDefinition(ctx, definition.CreateDefinitionRequest{
		Definition: &entities.ProcessDefinition{
			Project: &entities.Project{ID: projectID},
			Key:     req.Msg.Key,
			Name:    req.Msg.Name,
			Nodes:   nodes,
			Flows:   flows,
		},
	})
	if err != nil {
		return nil, err
	}
	resp := response.(definition.CreateDefinitionResponse)
	return connect.NewResponse(&pbendpoints.CreateDefinitionResponse{
		Id:    resp.ID.String(),
		Error: common.ErrString(resp.Err),
	}), nil
}

func (h *DefinitionHandler) ListDefinitions(ctx context.Context, req *connect.Request[pbendpoints.ListDefinitionsRequest]) (*connect.Response[pbendpoints.ListDefinitionsResponse], error) {
	response, err := h.eps.ListDefinitions(ctx, definition.ListDefinitionsRequest{
		ProjectID: req.Msg.ProjectId,
	})
	if err != nil {
		return nil, err
	}
	resp := response.(definition.ListDefinitionsResponse)
	pbDefs := make([]*pbentities.ProcessDefinition, len(resp.Definitions))
	for i, d := range resp.Definitions {
		pbDefs[i] = adapters.ProcessDefinitionPBAdapter{Definition: d}.ToProto()
	}
	return connect.NewResponse(&pbendpoints.ListDefinitionsResponse{
		Definitions: pbDefs,
		Error:       common.ErrString(resp.Err),
	}), nil
}

func (h *DefinitionHandler) GetDefinition(ctx context.Context, req *connect.Request[pbendpoints.GetDefinitionRequest]) (*connect.Response[pbendpoints.GetDefinitionResponse], error) {
	response, err := h.eps.GetDefinition(ctx, definition.GetDefinitionRequest{
		ID: req.Msg.Id,
	})
	if err != nil {
		return nil, err
	}
	resp := response.(definition.GetDefinitionResponse)
	return connect.NewResponse(&pbendpoints.GetDefinitionResponse{
		Definition: adapters.ProcessDefinitionPBAdapter{Definition: resp.Definition}.ToProto(),
		Error:      common.ErrString(resp.Err),
	}), nil
}

func (h *DefinitionHandler) DeleteDefinition(ctx context.Context, req *connect.Request[pbendpoints.DeleteDefinitionRequest]) (*connect.Response[pbendpoints.DeleteDefinitionResponse], error) {
	response, err := h.eps.DeleteDefinition(ctx, definition.DeleteDefinitionRequest{
		ID: req.Msg.Id,
	})
	if err != nil {
		return nil, err
	}
	resp := response.(definition.DeleteDefinitionResponse)
	return connect.NewResponse(&pbendpoints.DeleteDefinitionResponse{
		Error: common.ErrString(resp.Err),
	}), nil
}
