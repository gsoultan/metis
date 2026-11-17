package user

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
)

// CreateIntegrationKeyRequest is a key to make for the organization the
// request is for.
type CreateIntegrationKeyRequest struct {
	Name      string     `json:"name"`
	Roles     []string   `json:"roles"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// CreateIntegrationKeyResponse is the key made, and the key itself under
// "key": the only time it is shown.
type CreateIntegrationKeyResponse struct {
	entities.IntegrationAPIKey
	Key string `json:"key,omitempty"`
	Err error  `json:"err,omitempty"`
}

func (r CreateIntegrationKeyResponse) Failed() error { return r.Err }

// ListIntegrationKeysRequest asks for the organization's keys.
type ListIntegrationKeysRequest struct{}

type ListIntegrationKeysResponse struct {
	Keys []entities.IntegrationAPIKey `json:"keys"`
	Err  error                        `json:"err,omitempty"`
}

func (r ListIntegrationKeysResponse) Failed() error { return r.Err }

// RevokeIntegrationKeyRequest names the key to stop.
type RevokeIntegrationKeyRequest struct {
	ID uuid.UUID `json:"-"`
}

type RevokeIntegrationKeyResponse struct {
	Err error `json:"err,omitempty"`
}

func (r RevokeIntegrationKeyResponse) Failed() error { return r.Err }

// MakeCreateIntegrationKeyEndpoint makes a key for the organization the
// request is for.
func MakeCreateIntegrationKeyEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(CreateIntegrationKeyRequest)
		if !ok {
			return nil, fmt.Errorf("user: expected a CreateIntegrationKeyRequest, got %T", request)
		}
		key, token, err := s.CreateIntegrationKey(ctx, req.Name, req.Roles, req.ExpiresAt)
		if err != nil {
			return CreateIntegrationKeyResponse{Err: err}, nil
		}
		return CreateIntegrationKeyResponse{IntegrationAPIKey: key, Key: token}, nil
	}
}

// MakeListIntegrationKeysEndpoint lists the organization's keys.
func MakeListIntegrationKeysEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		if _, ok := request.(ListIntegrationKeysRequest); !ok {
			return nil, fmt.Errorf("user: expected a ListIntegrationKeysRequest, got %T", request)
		}
		keys, err := s.ListIntegrationKeys(ctx)
		if keys == nil {
			keys = []entities.IntegrationAPIKey{}
		}
		return ListIntegrationKeysResponse{Keys: keys, Err: err}, nil
	}
}

// MakeRevokeIntegrationKeyEndpoint stops one of the organization's keys.
func MakeRevokeIntegrationKeyEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(RevokeIntegrationKeyRequest)
		if !ok {
			return nil, fmt.Errorf("user: expected a RevokeIntegrationKeyRequest, got %T", request)
		}
		return RevokeIntegrationKeyResponse{Err: s.RevokeIntegrationKey(ctx, req.ID)}, nil
	}
}
