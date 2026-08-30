package user

import (
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

type GetUserRequest struct {
	ID uuid.UUID `json:"id"`
}

type GetUserResponse struct {
	User entities.User `json:"user"`
	Err  error         `json:"err,omitempty"`
}

func (r GetUserResponse) Failed() error { return r.Err }

type CreateUserRequest struct {
	User     entities.User `json:"user"`
	Password string        `json:"password"`
}

type CreateUserResponse struct {
	Err error `json:"err,omitempty"`
}

func (r CreateUserResponse) Failed() error { return r.Err }

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	User  any    `json:"user,omitempty"`
	Token string `json:"token"`
	Err   error  `json:"err,omitempty"`
}

func (r LoginResponse) Failed() error { return r.Err }

type ListUsersRequest struct {
	OrganizationID uuid.UUID `json:"organization_id"`
}

type ListUsersResponse struct {
	Users []entities.User `json:"users"`
	Err   error           `json:"err,omitempty"`
}

func (r ListUsersResponse) Failed() error { return r.Err }

type UpdateUserRequest struct {
	User entities.User `json:"user"`
}

type UpdateUserResponse struct {
	Err error `json:"err,omitempty"`
}

func (r UpdateUserResponse) Failed() error { return r.Err }

type DeleteUserRequest struct {
	ID uuid.UUID `json:"id"`
}

type DeleteUserResponse struct {
	Err error `json:"err,omitempty"`
}

func (r DeleteUserResponse) Failed() error { return r.Err }
