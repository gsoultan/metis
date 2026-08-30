package user

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/metis/server/domains/services"
)

type Endpoints struct {
	GetUser    endpoint.Endpoint
	CreateUser endpoint.Endpoint
	UpdateUser endpoint.Endpoint
	DeleteUser endpoint.Endpoint
	Login      endpoint.Endpoint
	ListUsers  endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		GetUser:    MakeGetUserEndpoint(s),
		CreateUser: MakeCreateUserEndpoint(s),
		UpdateUser: MakeUpdateUserEndpoint(s),
		DeleteUser: MakeDeleteUserEndpoint(s),
		Login:      MakeLoginEndpoint(s),
		ListUsers:  MakeListUsersEndpoint(s),
	}
}

func MakeGetUserEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(GetUserRequest)
		if !ok {
			return nil, fmt.Errorf("user: expected a GetUserRequest, got %T", request)
		}
		u, err := s.GetUser(ctx, req.ID)
		return GetUserResponse{User: u, Err: err}, nil
	}
}

func MakeCreateUserEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(CreateUserRequest)
		if !ok {
			return nil, fmt.Errorf("user: expected a CreateUserRequest, got %T", request)
		}
		err := s.CreateUser(ctx, req.User, req.Password)
		return CreateUserResponse{Err: err}, nil
	}
}

func MakeLoginEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(LoginRequest)
		if !ok {
			return nil, fmt.Errorf("user: expected a LoginRequest, got %T", request)
		}
		u, token, err := s.Login(ctx, req.Username, req.Password)
		if err != nil {
			return LoginResponse{Err: err}, nil
		}
		return LoginResponse{
			User: map[string]any{
				"id":       u.ID.String(),
				"name":     u.FullName,
				"username": u.Username,
				"role":     u.Roles,
			},
			Token: token,
		}, nil
	}
}

func MakeListUsersEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ListUsersRequest)
		if !ok {
			return nil, fmt.Errorf("user: expected a ListUsersRequest, got %T", request)
		}
		users, err := s.ListUsers(ctx, req.OrganizationID)
		return ListUsersResponse{Users: users, Err: err}, nil
	}
}

func MakeUpdateUserEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(UpdateUserRequest)
		if !ok {
			return nil, fmt.Errorf("user: expected a UpdateUserRequest, got %T", request)
		}
		err := s.UpdateUser(ctx, req.User)
		return UpdateUserResponse{Err: err}, nil
	}
}

func MakeDeleteUserEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DeleteUserRequest)
		if !ok {
			return nil, fmt.Errorf("user: expected a DeleteUserRequest, got %T", request)
		}
		err := s.DeleteUser(ctx, req.ID)
		return DeleteUserResponse{Err: err}, nil
	}
}
