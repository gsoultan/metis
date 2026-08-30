package groups

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	pbendpoints "github.com/gsoultan/metis/api/proto/endpoints"
	pbentities "github.com/gsoultan/metis/api/proto/entities"
	"github.com/gsoultan/metis/server/endpoints/group"
	"github.com/gsoultan/metis/server/transports/adapters"
)

type GroupHandler struct {
	eps group.Endpoints
}

func NewHandler(eps group.Endpoints) *GroupHandler {
	return &GroupHandler{eps: eps}
}

func (h *GroupHandler) ListGroups(ctx context.Context, req *connect.Request[pbendpoints.ListGroupsRequest]) (*connect.Response[pbendpoints.ListGroupsResponse], error) {
	orgID, err := uuid.Parse(req.Msg.OrganizationId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	response, err := h.eps.ListGroups(ctx, group.ListGroupsRequest{
		OrganizationID: orgID,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(group.ListGroupsResponse)
	if !ok {
		return nil, fmt.Errorf("groups: expected a group.ListGroupsResponse, got %T", response)
	}
	pbGroups := make([]*pbentities.Group, len(resp.Groups))
	for i, g := range resp.Groups {
		pbGroups[i] = adapters.GroupPBAdapter{Group: g}.ToProto()
	}
	return connect.NewResponse(&pbendpoints.ListGroupsResponse{
		Groups: pbGroups,
	}), nil
}

func (h *GroupHandler) ListUserGroups(ctx context.Context, req *connect.Request[pbendpoints.ListUserGroupsRequest]) (*connect.Response[pbendpoints.ListUserGroupsResponse], error) {
	userID, err := uuid.Parse(req.Msg.UserId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	response, err := h.eps.ListUserGroups(ctx, group.ListUserGroupsRequest{
		UserID: userID,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(group.ListUserGroupsResponse)
	if !ok {
		return nil, fmt.Errorf("groups: expected a group.ListUserGroupsResponse, got %T", response)
	}
	pbGroups := make([]*pbentities.Group, len(resp.Groups))
	for i, g := range resp.Groups {
		pbGroups[i] = adapters.GroupPBAdapter{Group: g}.ToProto()
	}
	return connect.NewResponse(&pbendpoints.ListUserGroupsResponse{
		Groups: pbGroups,
	}), nil
}
