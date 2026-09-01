package group

import (
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

type ListGroupsRequest struct {
	OrganizationID uuid.UUID `json:"organization_id"`
}

type ListGroupsResponse struct {
	Groups []entities.Group `json:"groups"`
	Err    error            `json:"err,omitempty"`
}

func (r ListGroupsResponse) Failed() error { return r.Err }

type CreateGroupRequest struct {
	Group entities.Group `json:"group"`
}

type CreateGroupResponse struct {
	Err error `json:"err,omitempty"`
}

func (r CreateGroupResponse) Failed() error { return r.Err }

type GetGroupRequest struct {
	ID uuid.UUID `json:"id"`
}

type GetGroupResponse struct {
	Group entities.Group `json:"group"`
	Err   error          `json:"err,omitempty"`
}

func (r GetGroupResponse) Failed() error { return r.Err }

type UpdateGroupRequest struct {
	Group entities.Group `json:"group"`
}

type UpdateGroupResponse struct {
	Err error `json:"err,omitempty"`
}

func (r UpdateGroupResponse) Failed() error { return r.Err }

type DeleteGroupRequest struct {
	ID uuid.UUID `json:"id"`
}

type DeleteGroupResponse struct {
	Err error `json:"err,omitempty"`
}

func (r DeleteGroupResponse) Failed() error { return r.Err }

type ListGroupMembersRequest struct {
	GroupID uuid.UUID `json:"group_id"`
}

type ListGroupMembersResponse struct {
	Users []entities.User `json:"users"`
	Err   error           `json:"err,omitempty"`
}

func (r ListGroupMembersResponse) Failed() error { return r.Err }

type AddMembershipRequest struct {
	UserID  uuid.UUID `json:"user_id"`
	GroupID uuid.UUID `json:"group_id"`
}

type AddMembershipResponse struct {
	Err error `json:"err,omitempty"`
}

func (r AddMembershipResponse) Failed() error { return r.Err }

type RemoveMembershipRequest struct {
	UserID  uuid.UUID `json:"user_id"`
	GroupID uuid.UUID `json:"group_id"`
}

type RemoveMembershipResponse struct {
	Err error `json:"err,omitempty"`
}

func (r RemoveMembershipResponse) Failed() error { return r.Err }

type ListUserGroupsRequest struct {
	UserID uuid.UUID `json:"user_id"`
}

type ListUserGroupsResponse struct {
	Groups []entities.Group `json:"groups"`
	Err    error            `json:"err,omitempty"`
}

func (r ListUserGroupsResponse) Failed() error { return r.Err }
