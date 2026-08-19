package users

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	pbendpoints "github.com/gsoultan/gobpm/api/proto/endpoints"
	pbentities "github.com/gsoultan/gobpm/api/proto/entities"
	"github.com/gsoultan/gobpm/server/endpoints/user"
	"github.com/gsoultan/gobpm/server/transports/adapters"
)

type UserHandler struct {
	eps user.Endpoints
}

func NewHandler(eps user.Endpoints) *UserHandler {
	return &UserHandler{eps: eps}
}

func (h *UserHandler) GetUser(ctx context.Context, req *connect.Request[pbendpoints.GetUserRequest]) (*connect.Response[pbendpoints.GetUserResponse], error) {
	id, err := uuid.Parse(req.Msg.Id)
	if err != nil {
		return nil, err
	}
	response, err := h.eps.GetUser(ctx, user.GetUserRequest{
		ID: id,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(user.GetUserResponse)
	if !ok {
		return nil, fmt.Errorf("users: expected a user.GetUserResponse, got %T", response)
	}
	return connect.NewResponse(&pbendpoints.GetUserResponse{
		User: adapters.UserPBAdapter{User: resp.User}.ToProto(),
	}), nil
}

func (h *UserHandler) ListUsers(ctx context.Context, req *connect.Request[pbendpoints.ListUsersRequest]) (*connect.Response[pbendpoints.ListUsersResponse], error) {
	var orgID uuid.UUID
	var err error
	if req.Msg.OrganizationId != "" {
		orgID, err = uuid.Parse(req.Msg.OrganizationId)
		if err != nil {
			return nil, err
		}
	}
	response, err := h.eps.ListUsers(ctx, user.ListUsersRequest{
		OrganizationID: orgID,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(user.ListUsersResponse)
	if !ok {
		return nil, fmt.Errorf("users: expected a user.ListUsersResponse, got %T", response)
	}
	pbUsers := make([]*pbentities.User, len(resp.Users))
	for i, u := range resp.Users {
		pbUsers[i] = adapters.UserPBAdapter{User: u}.ToProto()
	}
	return connect.NewResponse(&pbendpoints.ListUsersResponse{
		Users: pbUsers,
	}), nil
}

func (h *UserHandler) ListGroups(ctx context.Context, req *connect.Request[pbendpoints.ListGroupsRequest]) (*connect.Response[pbendpoints.ListGroupsResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

func (h *UserHandler) ListUserGroups(ctx context.Context, req *connect.Request[pbendpoints.ListUserGroupsRequest]) (*connect.Response[pbendpoints.ListUserGroupsResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}
