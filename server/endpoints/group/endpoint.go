package group

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/gobpm/server/domains/services"
)

type Endpoints struct {
	ListGroups       endpoint.Endpoint
	CreateGroup      endpoint.Endpoint
	GetGroup         endpoint.Endpoint
	UpdateGroup      endpoint.Endpoint
	DeleteGroup      endpoint.Endpoint
	ListGroupMembers endpoint.Endpoint
	AddMembership    endpoint.Endpoint
	RemoveMembership endpoint.Endpoint
	ListUserGroups   endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListGroups:       MakeListGroupsEndpoint(s),
		CreateGroup:      MakeCreateGroupEndpoint(s),
		GetGroup:         MakeGetGroupEndpoint(s),
		UpdateGroup:      MakeUpdateGroupEndpoint(s),
		DeleteGroup:      MakeDeleteGroupEndpoint(s),
		ListGroupMembers: MakeListGroupMembersEndpoint(s),
		AddMembership:    MakeAddMembershipEndpoint(s),
		RemoveMembership: MakeRemoveMembershipEndpoint(s),
		ListUserGroups:   MakeListUserGroupsEndpoint(s),
	}
}

func MakeListGroupsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListGroupsRequest)
		if !ok {
			return nil, fmt.Errorf("group: expected a ListGroupsRequest, got %T", request)
		}
		groups, err := s.ListGroups(ctx, req.OrganizationID)
		return ListGroupsResponse{Groups: groups, Err: err}, nil
	}
}

func MakeCreateGroupEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(CreateGroupRequest)
		if !ok {
			return nil, fmt.Errorf("group: expected a CreateGroupRequest, got %T", request)
		}
		err := s.CreateGroup(ctx, req.Group)
		return CreateGroupResponse{Err: err}, nil
	}
}

func MakeGetGroupEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(GetGroupRequest)
		if !ok {
			return nil, fmt.Errorf("group: expected a GetGroupRequest, got %T", request)
		}
		group, err := s.GetGroup(ctx, req.ID)
		return GetGroupResponse{Group: group, Err: err}, nil
	}
}

func MakeUpdateGroupEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(UpdateGroupRequest)
		if !ok {
			return nil, fmt.Errorf("group: expected a UpdateGroupRequest, got %T", request)
		}
		err := s.UpdateGroup(ctx, req.Group)
		return UpdateGroupResponse{Err: err}, nil
	}
}

func MakeDeleteGroupEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DeleteGroupRequest)
		if !ok {
			return nil, fmt.Errorf("group: expected a DeleteGroupRequest, got %T", request)
		}
		err := s.DeleteGroup(ctx, req.ID)
		return DeleteGroupResponse{Err: err}, nil
	}
}

func MakeListGroupMembersEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListGroupMembersRequest)
		if !ok {
			return nil, fmt.Errorf("group: expected a ListGroupMembersRequest, got %T", request)
		}
		users, err := s.ListGroupMembers(ctx, req.GroupID)
		return ListGroupMembersResponse{Users: users, Err: err}, nil
	}
}

func MakeAddMembershipEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(AddMembershipRequest)
		if !ok {
			return nil, fmt.Errorf("group: expected a AddMembershipRequest, got %T", request)
		}
		err := s.AddMembership(ctx, req.UserID, req.GroupID)
		return AddMembershipResponse{Err: err}, nil
	}
}

func MakeRemoveMembershipEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(RemoveMembershipRequest)
		if !ok {
			return nil, fmt.Errorf("group: expected a RemoveMembershipRequest, got %T", request)
		}
		err := s.RemoveMembership(ctx, req.UserID, req.GroupID)
		return RemoveMembershipResponse{Err: err}, nil
	}
}

func MakeListUserGroupsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListUserGroupsRequest)
		if !ok {
			return nil, fmt.Errorf("group: expected a ListUserGroupsRequest, got %T", request)
		}
		groups, err := s.ListUserGroups(ctx, req.UserID)
		return ListUserGroupsResponse{Groups: groups, Err: err}, nil
	}
}
