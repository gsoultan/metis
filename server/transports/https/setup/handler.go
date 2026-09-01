package setup

import (
	"context"
	"encoding/json"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/metis/server/endpoints/setup"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps setup.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/setup/status", httptransport.NewServer(
		eps.GetSetupStatusEndpoint,
		decodeGetSetupStatusRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/setup", httptransport.NewServer(
		eps.SetupEndpoint,
		decodeSetupRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/setup/test-connection", httptransport.NewServer(
		eps.TestConnectionEndpoint,
		decodeTestConnectionRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeGetSetupStatusRequest(_ context.Context, _ *http.Request) (any, error) {
	return setup.GetSetupStatusRequest{}, nil
}

func decodeSetupRequest(_ context.Context, r *http.Request) (any, error) {
	var req setup.SetupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeTestConnectionRequest(_ context.Context, r *http.Request) (any, error) {
	var req setup.TestConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}
