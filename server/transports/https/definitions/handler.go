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
	// The script-task inventory: every script task in the tenant, with its body.
	// A literal segment for the same reason as the worklist above.
	m.Handle("GET /api/v1/definitions/script-tasks", httptransport.NewServer(
		eps.ListScriptTasks,
		decodeListScriptTasksRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/definitions/import", httptransport.NewServer(
		eps.ImportDefinition,
		decodeImportDefinitionRequest,
		common.EncodeResponse,
		options...,
	))

	// Which version of each key is live, for a page that lists many processes.
	m.Handle("GET /api/v1/definitions/live-versions", httptransport.NewServer(
		eps.ListLiveVersions,
		decodeListLiveVersionsRequest,
		common.EncodeResponse,
		options...,
	))

	// Version history for one process key, with which version is live and how
	// much work each one still holds.
	//
	// The key travels as a query parameter rather than a path segment. A
	// two-segment pattern here — /definitions/versions/{key} — is refused by
	// ServeMux at registration: it and /definitions/{id}/export both match
	// "/definitions/versions/export" and neither is more specific, so the whole
	// server panics on boot rather than answering that one route wrongly.
	m.Handle("GET /api/v1/definitions/versions", httptransport.NewServer(
		eps.ListDefinitionVersions,
		decodeListDefinitionVersionsRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/definitions/versions/promote", httptransport.NewServer(
		eps.PromoteDefinition,
		decodePromoteDefinitionRequest,
		common.EncodeResponse,
		options...,
	))

	// Arranging a cutover for later, and dropping one that has not happened yet.
	// All-literal segments, so neither can be read as a definition id.
	m.Handle("POST /api/v1/definitions/versions/schedule", httptransport.NewServer(
		eps.ScheduleDefinition,
		decodeScheduleDefinitionRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/definitions/versions/schedule/cancel", httptransport.NewServer(
		eps.CancelScheduledDefinition,
		decodeCancelScheduledDefinitionRequest,
		common.EncodeResponse,
		options...,
	))

	// Moving work already in flight onto another version. All-literal segments
	// for the same reason as the two above: a wildcard here could match
	// /definitions/{id}/... and ServeMux refuses ambiguous patterns at
	// registration, which takes the whole server down rather than one route.
	m.Handle("POST /api/v1/definitions/versions/migrate", httptransport.NewServer(
		eps.MigrateInstances,
		decodeMigrateInstancesRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeMigrateInstancesRequest(_ context.Context, r *http.Request) (any, error) {
	var req definition.MigrateInstancesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeListLiveVersionsRequest(_ context.Context, r *http.Request) (any, error) {
	return definition.ListLiveVersionsRequest{ProjectID: r.URL.Query().Get("project_id")}, nil
}

func decodeListDefinitionVersionsRequest(_ context.Context, r *http.Request) (any, error) {
	return definition.ListDefinitionVersionsRequest{
		ProjectID: r.URL.Query().Get("project_id"),
		Key:       r.URL.Query().Get("key"),
	}, nil
}

func decodePromoteDefinitionRequest(_ context.Context, r *http.Request) (any, error) {
	var body struct {
		ProjectID string `json:"project_id"`
		Key       string `json:"key"`
		Version   int    `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, err
	}
	return definition.PromoteDefinitionRequest{
		ProjectID: body.ProjectID,
		Key:       body.Key,
		Version:   body.Version,
	}, nil
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

func decodeListScriptTasksRequest(_ context.Context, _ *http.Request) (any, error) {
	return definition.ListScriptTasksRequest{}, nil
}

func decodeImportDefinitionRequest(_ context.Context, r *http.Request) (any, error) {
	var req definition.ImportDefinitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeScheduleDefinitionRequest(_ context.Context, r *http.Request) (any, error) {
	var req definition.ScheduleDefinitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeCancelScheduledDefinitionRequest(_ context.Context, r *http.Request) (any, error) {
	var req definition.CancelScheduledDefinitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}
