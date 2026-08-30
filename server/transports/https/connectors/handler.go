package connectors

import (
	"context"
	"encoding/json"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/metis/server/endpoints/connector"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps connector.Endpoints, options []httptransport.ServerOption) {
	// Connectors written as a document rather than compiled in.
	m.Handle("POST /api/v1/connector-manifests", httptransport.NewServer(
		eps.InstallManifest, decodeInstallManifest, common.EncodeResponse, options...))
	m.Handle("GET /api/v1/connector-manifests", httptransport.NewServer(
		eps.ListManifests, decodeListManifests, common.EncodeResponse, options...))
	m.Handle("GET /api/v1/connector-manifests/{key}", httptransport.NewServer(
		eps.GetManifest, decodeGetManifest, common.EncodeResponse, options...))
	m.Handle("POST /api/v1/connector-manifests/{id}/enabled", httptransport.NewServer(
		eps.SetManifestEnabled, decodeSetManifestEnabled, common.EncodeResponse, options...))
	m.Handle("DELETE /api/v1/connector-manifests/{id}", httptransport.NewServer(
		eps.DeleteManifest, decodeDeleteManifest, common.EncodeResponse, options...))

	m.Handle("GET /api/v1/connectors", httptransport.NewServer(
		eps.ListConnectors,
		decodeListConnectorsRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/connectors", httptransport.NewServer(
		eps.CreateConnector,
		decodeCreateConnectorRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("PUT /api/v1/connectors/{id}", httptransport.NewServer(
		eps.UpdateConnector,
		decodeUpdateConnectorRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("DELETE /api/v1/connectors/{id}", httptransport.NewServer(
		eps.DeleteConnector,
		decodeDeleteConnectorRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/connectors/instances", httptransport.NewServer(
		eps.ListConnectorInstances,
		decodeListConnectorInstancesRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/connectors/instances", httptransport.NewServer(
		eps.CreateConnectorInstance,
		decodeCreateConnectorInstanceRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("PUT /api/v1/connectors/instances/{id}", httptransport.NewServer(
		eps.UpdateConnectorInstance,
		decodeUpdateConnectorInstanceRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("DELETE /api/v1/connectors/instances/{id}", httptransport.NewServer(
		eps.DeleteConnectorInstance,
		decodeDeleteConnectorInstanceRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/connectors/execute", httptransport.NewServer(
		eps.ExecuteConnector,
		decodeExecuteConnectorRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeListConnectorsRequest(_ context.Context, _ *http.Request) (any, error) {
	return connector.ListConnectorsRequest{}, nil
}

func decodeCreateConnectorRequest(_ context.Context, r *http.Request) (any, error) {
	var req connector.CreateConnectorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeUpdateConnectorRequest(_ context.Context, r *http.Request) (any, error) {
	var req connector.UpdateConnectorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeDeleteConnectorRequest(_ context.Context, r *http.Request) (any, error) {
	return connector.DeleteConnectorRequest{ID: r.PathValue("id")}, nil
}

func decodeListConnectorInstancesRequest(_ context.Context, r *http.Request) (any, error) {
	return connector.ListConnectorInstancesRequest{ProjectID: r.URL.Query().Get("project_id")}, nil
}

func decodeCreateConnectorInstanceRequest(_ context.Context, r *http.Request) (any, error) {
	var req connector.CreateConnectorInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeUpdateConnectorInstanceRequest(_ context.Context, r *http.Request) (any, error) {
	var req connector.UpdateConnectorInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeDeleteConnectorInstanceRequest(_ context.Context, r *http.Request) (any, error) {
	return connector.DeleteConnectorInstanceRequest{ID: r.PathValue("id")}, nil
}

func decodeExecuteConnectorRequest(_ context.Context, r *http.Request) (any, error) {
	var req connector.ExecuteConnectorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeInstallManifest(_ context.Context, r *http.Request) (any, error) {
	var req connector.InstallManifestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeListManifests(_ context.Context, _ *http.Request) (any, error) {
	return connector.ListManifestsRequest{}, nil
}

func decodeGetManifest(_ context.Context, r *http.Request) (any, error) {
	return connector.GetManifestRequest{Key: r.PathValue("key")}, nil
}

func decodeSetManifestEnabled(_ context.Context, r *http.Request) (any, error) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, err
	}
	return connector.SetManifestEnabledRequest{ID: r.PathValue("id"), Enabled: body.Enabled}, nil
}

func decodeDeleteManifest(_ context.Context, r *http.Request) (any, error) {
	return connector.DeleteManifestRequest{ID: r.PathValue("id")}, nil
}
