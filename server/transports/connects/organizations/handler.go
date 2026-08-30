package organizations

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	pbendpoints "github.com/gsoultan/metis/api/proto/endpoints"
	pbentities "github.com/gsoultan/metis/api/proto/entities"
	"github.com/gsoultan/metis/server/endpoints/organization"
	"github.com/gsoultan/metis/server/transports/adapters"
	"github.com/gsoultan/metis/server/transports/grpcs/common"
)

type OrganizationHandler struct {
	eps organization.Endpoints
}

func NewHandler(eps organization.Endpoints) *OrganizationHandler {
	return &OrganizationHandler{eps: eps}
}

func (h *OrganizationHandler) CreateOrganization(ctx context.Context, req *connect.Request[pbendpoints.CreateOrganizationRequest]) (*connect.Response[pbendpoints.CreateOrganizationResponse], error) {
	response, err := h.eps.CreateOrganization(ctx, organization.CreateOrganizationRequest{
		Name:        req.Msg.Name,
		Description: req.Msg.Description,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(organization.CreateOrganizationResponse)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a organization.CreateOrganizationResponse, got %T", response)
	}
	return connect.NewResponse(&pbendpoints.CreateOrganizationResponse{
		Organization: adapters.OrganizationPBAdapter{Organization: resp.Organization}.ToProto(),
		Error:        common.ErrString(resp.Err),
	}), nil
}

func (h *OrganizationHandler) GetOrganization(ctx context.Context, req *connect.Request[pbendpoints.GetOrganizationRequest]) (*connect.Response[pbendpoints.GetOrganizationResponse], error) {
	response, err := h.eps.GetOrganization(ctx, organization.GetOrganizationRequest{
		ID: req.Msg.Id,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(organization.GetOrganizationResponse)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a organization.GetOrganizationResponse, got %T", response)
	}
	return connect.NewResponse(&pbendpoints.GetOrganizationResponse{
		Organization: adapters.OrganizationPBAdapter{Organization: resp.Organization}.ToProto(),
		Error:        common.ErrString(resp.Err),
	}), nil
}

func (h *OrganizationHandler) ListOrganizations(ctx context.Context, _ *connect.Request[pbendpoints.ListOrganizationsRequest]) (*connect.Response[pbendpoints.ListOrganizationsResponse], error) {
	response, err := h.eps.ListOrganizations(ctx, organization.ListOrganizationsRequest{})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(organization.ListOrganizationsResponse)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a organization.ListOrganizationsResponse, got %T", response)
	}
	pbOrgs := make([]*pbentities.Organization, len(resp.Organizations))
	for i, o := range resp.Organizations {
		pbOrgs[i] = adapters.OrganizationPBAdapter{Organization: o}.ToProto()
	}
	return connect.NewResponse(&pbendpoints.ListOrganizationsResponse{
		Organizations: pbOrgs,
		Error:         common.ErrString(resp.Err),
	}), nil
}

func (h *OrganizationHandler) UpdateOrganization(ctx context.Context, req *connect.Request[pbendpoints.UpdateOrganizationRequest]) (*connect.Response[pbendpoints.UpdateOrganizationResponse], error) {
	response, err := h.eps.UpdateOrganization(ctx, organization.UpdateOrganizationRequest{
		ID:          req.Msg.Id,
		Name:        req.Msg.Name,
		Description: req.Msg.Description,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(organization.UpdateOrganizationResponse)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a organization.UpdateOrganizationResponse, got %T", response)
	}
	return connect.NewResponse(&pbendpoints.UpdateOrganizationResponse{
		Error: common.ErrString(resp.Err),
	}), nil
}

func (h *OrganizationHandler) DeleteOrganization(ctx context.Context, req *connect.Request[pbendpoints.DeleteOrganizationRequest]) (*connect.Response[pbendpoints.DeleteOrganizationResponse], error) {
	response, err := h.eps.DeleteOrganization(ctx, organization.DeleteOrganizationRequest{
		ID: req.Msg.Id,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := response.(organization.DeleteOrganizationResponse)
	if !ok {
		return nil, fmt.Errorf("organizations: expected a organization.DeleteOrganizationResponse, got %T", response)
	}
	return connect.NewResponse(&pbendpoints.DeleteOrganizationResponse{
		Error: common.ErrString(resp.Err),
	}), nil
}
