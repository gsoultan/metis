package group

import (
	"context"
	"encoding/json"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/endpoints/group"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps group.Endpoints, options []httptransport.ServerOption) {
	// Groups
	m.Handle("POST /api/v1/organizations/{organization_id}/groups", httptransport.NewServer(
		eps.CreateGroup,
		decodeCreateGroupRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/organizations/{organization_id}/groups", httptransport.NewServer(
		eps.ListGroups,
		decodeListGroupsRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/organizations/groups", httptransport.NewServer(
		eps.ListGroups,
		decodeListAllGroupsRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/groups/{id}", httptransport.NewServer(
		eps.GetGroup,
		decodeGetGroupRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("PUT /api/v1/groups/{id}", httptransport.NewServer(
		eps.UpdateGroup,
		decodeUpdateGroupRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("DELETE /api/v1/groups/{id}", httptransport.NewServer(
		eps.DeleteGroup,
		decodeDeleteGroupRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/groups/{id}/members", httptransport.NewServer(
		eps.ListGroupMembers,
		decodeListGroupMembersRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/groups/{id}/members/{user_id}", httptransport.NewServer(
		eps.AddMembership,
		decodeAddMembershipRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("DELETE /api/v1/groups/{id}/members/{user_id}", httptransport.NewServer(
		eps.RemoveMembership,
		decodeRemoveMembershipRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/users/{id}/groups", httptransport.NewServer(
		eps.ListUserGroups,
		decodeListUserGroupsRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeCreateGroupRequest(_ context.Context, r *http.Request) (any, error) {
	var req group.CreateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	orgID, err := uuid.Parse(r.PathValue("organization_id"))
	if err != nil {
		return nil, err
	}
	req.Group.Organization = &entities.Organization{ID: orgID}
	return req, nil
}

func decodeListGroupsRequest(_ context.Context, r *http.Request) (any, error) {
	orgID, err := uuid.Parse(r.PathValue("organization_id"))
	if err != nil {
		return nil, err
	}
	return group.ListGroupsRequest{OrganizationID: orgID}, nil
}

func decodeListAllGroupsRequest(_ context.Context, _ *http.Request) (any, error) {
	return group.ListGroupsRequest{}, nil
}

func decodeGetGroupRequest(_ context.Context, r *http.Request) (any, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return group.GetGroupRequest{ID: id}, nil
}

func decodeUpdateGroupRequest(_ context.Context, r *http.Request) (any, error) {
	var req group.UpdateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	req.Group.ID = id
	return req, nil
}

func decodeDeleteGroupRequest(_ context.Context, r *http.Request) (any, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return group.DeleteGroupRequest{ID: id}, nil
}

func decodeListGroupMembersRequest(_ context.Context, r *http.Request) (any, error) {
	groupID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return group.ListGroupMembersRequest{GroupID: groupID}, nil
}

func decodeAddMembershipRequest(_ context.Context, r *http.Request) (any, error) {
	groupID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	userID, err := uuid.Parse(r.PathValue("user_id"))
	if err != nil {
		return nil, err
	}
	return group.AddMembershipRequest{UserID: userID, GroupID: groupID}, nil
}

func decodeRemoveMembershipRequest(_ context.Context, r *http.Request) (any, error) {
	groupID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	userID, err := uuid.Parse(r.PathValue("user_id"))
	if err != nil {
		return nil, err
	}
	return group.RemoveMembershipRequest{UserID: userID, GroupID: groupID}, nil
}

func decodeListUserGroupsRequest(_ context.Context, r *http.Request) (any, error) {
	userID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return group.ListUserGroupsRequest{UserID: userID}, nil
}
