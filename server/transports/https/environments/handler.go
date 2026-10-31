package environments

import (
	"context"
	"encoding/json"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/metis/server/endpoints/environment"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// RegisterHandlers mounts the environment registry.
//
// Under /environments rather than /projects/{id}/environments: the project
// arrives as a query parameter, which keeps every route here a single literal
// segment and out of the way of the {id} routes elsewhere. Two-segment patterns
// mixing a literal and a wildcard are what ServeMux refuses at registration,
// taking the whole server down rather than one route.
func RegisterHandlers(m *http.ServeMux, eps environment.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/environments", httptransport.NewServer(
		eps.ListEnvironments,
		decodeListEnvironmentsRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/environments", httptransport.NewServer(
		eps.SaveEnvironment,
		decodeSaveEnvironmentRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/environments/current", httptransport.NewServer(
		eps.CurrentRuntime,
		decodeListEnvironmentsRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/environments/test-connection", httptransport.NewServer(
		eps.TestConnection,
		decodeTestConnectionRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("DELETE /api/v1/environments/{id}", httptransport.NewServer(
		eps.DeleteEnvironment,
		decodeDeleteEnvironmentRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeListEnvironmentsRequest(_ context.Context, r *http.Request) (any, error) {
	return environment.ListEnvironmentsRequest{ProjectID: r.URL.Query().Get("project_id")}, nil
}

func decodeSaveEnvironmentRequest(_ context.Context, r *http.Request) (any, error) {
	var req environment.SaveEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeDeleteEnvironmentRequest(_ context.Context, r *http.Request) (any, error) {
	return environment.DeleteEnvironmentRequest{ID: r.PathValue("id")}, nil
}

func decodeTestConnectionRequest(_ context.Context, r *http.Request) (any, error) {
	var req environment.TestEnvironmentConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}
