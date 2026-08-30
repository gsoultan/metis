package organizations

import (
	"context"
	"encoding/json"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/metis/server/endpoints/organization"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps organization.Endpoints, options []httptransport.ServerOption) {
	m.Handle("POST /api/v1/organizations", httptransport.NewServer(
		eps.CreateOrganization,
		decodeCreateOrganizationRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/organizations", httptransport.NewServer(
		eps.ListOrganizations,
		decodeListOrganizationsRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/organizations/{id}", httptransport.NewServer(
		eps.GetOrganization,
		decodeGetOrganizationRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("PUT /api/v1/organizations/{id}", httptransport.NewServer(
		eps.UpdateOrganization,
		decodeUpdateOrganizationRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("DELETE /api/v1/organizations/{id}", httptransport.NewServer(
		eps.DeleteOrganization,
		decodeDeleteOrganizationRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeCreateOrganizationRequest(_ context.Context, r *http.Request) (any, error) {
	var req organization.CreateOrganizationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeListOrganizationsRequest(_ context.Context, _ *http.Request) (any, error) {
	return organization.ListOrganizationsRequest{}, nil
}

func decodeGetOrganizationRequest(_ context.Context, r *http.Request) (any, error) {
	id := r.PathValue("id")
	return organization.GetOrganizationRequest{ID: id}, nil
}

func decodeUpdateOrganizationRequest(_ context.Context, r *http.Request) (any, error) {
	var req organization.UpdateOrganizationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	req.ID = r.PathValue("id")
	return req, nil
}

func decodeDeleteOrganizationRequest(_ context.Context, r *http.Request) (any, error) {
	id := r.PathValue("id")
	return organization.DeleteOrganizationRequest{ID: id}, nil
}
