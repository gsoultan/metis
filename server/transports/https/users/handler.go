package users

import (
	"context"
	"encoding/json"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/endpoints/user"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps user.Endpoints, options []httptransport.ServerOption) {
	m.Handle("POST /api/v1/login", httptransport.NewServer(
		eps.Login,
		decodeLoginRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/users", httptransport.NewServer(
		eps.CreateUser,
		decodeCreateUserRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/users/{id}", httptransport.NewServer(
		eps.GetUser,
		decodeGetUserRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/organizations/{organization_id}/users", httptransport.NewServer(
		eps.ListUsers,
		decodeListUsersRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("PUT /api/v1/users/{id}", httptransport.NewServer(
		eps.UpdateUser,
		decodeUpdateUserRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("DELETE /api/v1/users/{id}", httptransport.NewServer(
		eps.DeleteUser,
		decodeDeleteUserRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeLoginRequest(_ context.Context, r *http.Request) (any, error) {
	var req user.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeCreateUserRequest(_ context.Context, r *http.Request) (any, error) {
	var req user.CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeGetUserRequest(_ context.Context, r *http.Request) (any, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return user.GetUserRequest{ID: id}, nil
}

func decodeListUsersRequest(_ context.Context, r *http.Request) (any, error) {
	orgID, err := uuid.Parse(r.PathValue("organization_id"))
	if err != nil {
		return nil, err
	}
	return user.ListUsersRequest{OrganizationID: orgID}, nil
}

func decodeUpdateUserRequest(_ context.Context, r *http.Request) (any, error) {
	var req user.UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	req.User.ID = id
	return req, nil
}

func decodeDeleteUserRequest(_ context.Context, r *http.Request) (any, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return user.DeleteUserRequest{ID: id}, nil
}
