package definitions

import (
	"context"
	"encoding/json"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/metis/server/endpoints/definition"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps definition.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/definitions", httptransport.NewServer(
		eps.ListDefinitions,
		decodeListDefinitionsRequest,
		common.EncodeResponse,
		options...,
	))

	m.Handle("POST /api/v1/definitions", httptransport.NewServer(
		eps.CreateDefinition,
		decodeCreateDefinitionRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("DELETE /api/v1/definitions/{id}", httptransport.NewServer(
		eps.DeleteDefinition,
		decodeDeleteDefinitionRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/definitions/{id}/export", httptransport.NewServer(
		eps.ExportDefinition,
		decodeExportDefinitionRequest,
		common.EncodeResponse,
		options...,
	))
	// The javascript-conditions worklist: which stored conditions the flag
	// still gates. A literal segment, so it cannot collide with {id} routes.
	m.Handle("GET /api/v1/definitions/javascript-conditions", httptransport.NewServer(
		eps.ListJavaScriptConditions,
		decodeListJavaScriptConditionsRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/definitions/import", httptransport.NewServer(
		eps.ImportDefinition,
		decodeImportDefinitionRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeListDefinitionsRequest(_ context.Context, r *http.Request) (any, error) {
	page, pageSize := common.PageParams(r)
	return definition.ListDefinitionsRequest{
		ProjectID: r.URL.Query().Get("project_id"),
		Page:      page,
		PageSize:  pageSize,
	}, nil
}

func decodeCreateDefinitionRequest(_ context.Context, r *http.Request) (any, error) {
	var req definition.CreateDefinitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeDeleteDefinitionRequest(_ context.Context, r *http.Request) (any, error) {
	return definition.DeleteDefinitionRequest{ID: r.PathValue("id")}, nil
}

func decodeExportDefinitionRequest(_ context.Context, r *http.Request) (any, error) {
	return definition.ExportDefinitionRequest{ID: r.PathValue("id")}, nil
}

func decodeListJavaScriptConditionsRequest(_ context.Context, _ *http.Request) (any, error) {
	return definition.ListJavaScriptConditionsRequest{}, nil
}

func decodeImportDefinitionRequest(_ context.Context, r *http.Request) (any, error) {
	var req definition.ImportDefinitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}
