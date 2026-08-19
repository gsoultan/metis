package organizations

import (
	"context"
	"fmt"

	grpctransport "github.com/go-kit/kit/transport/grpc"
	"github.com/gsoultan/gobpm/api/proto/endpoints"
	"github.com/gsoultan/gobpm/api/proto/entities"
	"github.com/gsoultan/gobpm/api/proto/services"
	"github.com/gsoultan/gobpm/server/endpoints/organization"
	"github.com/gsoultan/gobpm/server/transports/adapters"
	"github.com/gsoultan/gobpm/server/transports/grpcs/common"
)

type Server struct {
	services.UnimplementedOrganizationServiceServer
	createOrganization grpctransport.Handler
	getOrganization    grpctransport.Handler
	listOrganizations  grpctransport.Handler
	updateOrganization grpctransport.Handler
	deleteOrganization grpctransport.Handler
}

func NewServer(eps organization.Endpoints) *Server {
	return &Server{
		createOrganization: grpctransport.NewServer(
			eps.CreateOrganization,
			decodeGRPCCreateOrganizationRequest,
			encodeGRPCCreateOrganizationResponse,
		),
		getOrganization: grpctransport.NewServer(
			eps.GetOrganization,
			decodeGRPCGetOrganizationRequest,
			encodeGRPCGetOrganizationResponse,
		),
		listOrganizations: grpctransport.NewServer(
			eps.ListOrganizations,
			decodeGRPCListOrganizationsRequest,
			encodeGRPCListOrganizationsResponse,
		),
		updateOrganization: grpctransport.NewServer(
			eps.UpdateOrganization,
			decodeGRPCUpdateOrganizationRequest,
			encodeGRPCUpdateOrganizationResponse,
		),
		deleteOrganization: grpctransport.NewServer(
			eps.DeleteOrganization,
			decodeGRPCDeleteOrganizationRequest,
			encodeGRPCDeleteOrganizationResponse,
		),
	}
}

func (s *Server) CreateOrganization(ctx context.Context, req *endpoints.CreateOrganizationRequest) (*endpoints.CreateOrganizationResponse, error) {
	_, resp, err := s.createOrganization.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.CreateOrganizationResponse)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a *endpoints.CreateOrganizationResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) GetOrganization(ctx context.Context, req *endpoints.GetOrganizationRequest) (*endpoints.GetOrganizationResponse, error) {
	_, resp, err := s.getOrganization.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.GetOrganizationResponse)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a *endpoints.GetOrganizationResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) ListOrganizations(ctx context.Context, req *endpoints.ListOrganizationsRequest) (*endpoints.ListOrganizationsResponse, error) {
	_, resp, err := s.listOrganizations.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.ListOrganizationsResponse)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a *endpoints.ListOrganizationsResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) UpdateOrganization(ctx context.Context, req *endpoints.UpdateOrganizationRequest) (*endpoints.UpdateOrganizationResponse, error) {
	_, resp, err := s.updateOrganization.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.UpdateOrganizationResponse)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a *endpoints.UpdateOrganizationResponse, got %T", resp)
	}
	return typed, nil
}

func (s *Server) DeleteOrganization(ctx context.Context, req *endpoints.DeleteOrganizationRequest) (*endpoints.DeleteOrganizationResponse, error) {
	_, resp, err := s.deleteOrganization.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*endpoints.DeleteOrganizationResponse)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a *endpoints.DeleteOrganizationResponse, got %T", resp)
	}
	return typed, nil
}

func decodeGRPCCreateOrganizationRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.CreateOrganizationRequest)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a *endpoints.CreateOrganizationRequest, got %T", grpcReq)
	}
	return organization.CreateOrganizationRequest{Name: req.Name, Description: req.Description}, nil
}

func encodeGRPCCreateOrganizationResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(organization.CreateOrganizationResponse)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a organization.CreateOrganizationResponse, got %T", response)
	}
	return &endpoints.CreateOrganizationResponse{
		Organization: adapters.OrganizationPBAdapter{Organization: resp.Organization}.ToProto(),
		Error:        common.ErrString(resp.Err),
	}, nil
}

func decodeGRPCGetOrganizationRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.GetOrganizationRequest)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a *endpoints.GetOrganizationRequest, got %T", grpcReq)
	}
	return organization.GetOrganizationRequest{ID: req.Id}, nil
}

func encodeGRPCGetOrganizationResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(organization.GetOrganizationResponse)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a organization.GetOrganizationResponse, got %T", response)
	}
	return &endpoints.GetOrganizationResponse{
		Organization: adapters.OrganizationPBAdapter{Organization: resp.Organization}.ToProto(),
		Error:        common.ErrString(resp.Err),
	}, nil
}

func decodeGRPCListOrganizationsRequest(_ context.Context, _ any) (any, error) {
	return organization.ListOrganizationsRequest{}, nil
}

func encodeGRPCListOrganizationsResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(organization.ListOrganizationsResponse)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a organization.ListOrganizationsResponse, got %T", response)
	}
	var orgs []*entities.Organization
	if len(resp.Organizations) > 0 {
		orgs = make([]*entities.Organization, 0, len(resp.Organizations))
		for _, o := range resp.Organizations {
			orgs = append(orgs, adapters.OrganizationPBAdapter{Organization: o}.ToProto())
		}
	}
	return &endpoints.ListOrganizationsResponse{Organizations: orgs, Error: common.ErrString(resp.Err)}, nil
}

func decodeGRPCUpdateOrganizationRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.UpdateOrganizationRequest)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a *endpoints.UpdateOrganizationRequest, got %T", grpcReq)
	}
	return organization.UpdateOrganizationRequest{ID: req.Id, Name: req.Name, Description: req.Description}, nil
}

func encodeGRPCUpdateOrganizationResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(organization.UpdateOrganizationResponse)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a organization.UpdateOrganizationResponse, got %T", response)
	}
	return &endpoints.UpdateOrganizationResponse{Error: common.ErrString(resp.Err)}, nil
}

func decodeGRPCDeleteOrganizationRequest(_ context.Context, grpcReq any) (any, error) {
	req, ok := grpcReq.(*endpoints.DeleteOrganizationRequest)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a *endpoints.DeleteOrganizationRequest, got %T", grpcReq)
	}
	return organization.DeleteOrganizationRequest{ID: req.Id}, nil
}

func encodeGRPCDeleteOrganizationResponse(_ context.Context, response any) (any, error) {
	resp, ok := response.(organization.DeleteOrganizationResponse)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a organization.DeleteOrganizationResponse, got %T", response)
	}
	return &endpoints.DeleteOrganizationResponse{Error: common.ErrString(resp.Err)}, nil
}
