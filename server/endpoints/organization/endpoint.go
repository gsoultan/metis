package organization

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/internal/pkg/apierr"
	"github.com/gsoultan/gobpm/server/domains/services"
)

type Endpoints struct {
	CreateOrganization endpoint.Endpoint
	GetOrganization    endpoint.Endpoint
	ListOrganizations  endpoint.Endpoint
	UpdateOrganization endpoint.Endpoint
	DeleteOrganization endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		CreateOrganization: MakeCreateOrganizationEndpoint(s),
		GetOrganization:    MakeGetOrganizationEndpoint(s),
		ListOrganizations:  MakeListOrganizationsEndpoint(s),
		UpdateOrganization: MakeUpdateOrganizationEndpoint(s),
		DeleteOrganization: MakeDeleteOrganizationEndpoint(s),
	}
}

func MakeCreateOrganizationEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(CreateOrganizationRequest)
		if !ok {
			return nil, fmt.Errorf("organization: expected a CreateOrganizationRequest, got %T", request)
		}
		o, err := s.CreateOrganization(ctx, req.Name, req.Description)
		return CreateOrganizationResponse{Organization: o, Err: err}, nil
	}
}

func MakeGetOrganizationEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(GetOrganizationRequest)
		if !ok {
			return nil, fmt.Errorf("organization: expected a GetOrganizationRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return GetOrganizationResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		o, err := s.GetOrganization(ctx, id)
		return GetOrganizationResponse{Organization: o, Err: err}, nil
	}
}

func MakeListOrganizationsEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		orgs, err := s.ListOrganizations(ctx)
		return ListOrganizationsResponse{Organizations: orgs, Err: err}, nil
	}
}

func MakeUpdateOrganizationEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(UpdateOrganizationRequest)
		if !ok {
			return nil, fmt.Errorf("organization: expected a UpdateOrganizationRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return UpdateOrganizationResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		err = s.UpdateOrganization(ctx, id, req.Name, req.Description)
		return UpdateOrganizationResponse{Err: err}, nil
	}
}

func MakeDeleteOrganizationEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(DeleteOrganizationRequest)
		if !ok {
			return nil, fmt.Errorf("organization: expected a DeleteOrganizationRequest, got %T", request)
		}
		id, err := uuid.Parse(req.ID)
		if err != nil {
			return DeleteOrganizationResponse{Err: apierr.Invalidf("id %q is not a valid identifier: %v", req.ID, err)}, nil
		}
		err = s.DeleteOrganization(ctx, id)
		return DeleteOrganizationResponse{Err: err}, nil
	}
}
