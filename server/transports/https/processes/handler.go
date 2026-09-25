package processes

import (
	"context"
	"encoding/json"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/metis/server/endpoints/process"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps process.Endpoints, options []httptransport.ServerOption) {
	m.Handle("POST /api/v1/process/start", httptransport.NewServer(
		eps.StartProcess,
		decodeStartProcessRequest,
		common.EncodeResponse,
		options...,
	))

	m.Handle("GET /api/v1/instances", httptransport.NewServer(
		eps.ListInstances,
		decodeListInstancesRequest,
		common.EncodeResponse,
		options...,
	))

	m.Handle("GET /api/v1/instances/{id}", httptransport.NewServer(
		eps.GetInstance,
		decodeGetInstanceRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/instances/{id}/path", httptransport.NewServer(
		eps.GetExecutionPath,
		decodeGetExecutionPathRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/instances/{id}/audit", httptransport.NewServer(
		eps.GetAuditLogs,
		decodeGetAuditLogsRequest,
		common.EncodeResponse,
		options...,
	))
	// The object-centric event log for a whole project. It is a project-level
	// read rather than an instance-level one because a mined model is only
	// meaningful across cases, and the tenant scope is applied by the
	// repository the same way it is for every other project-scoped read.
	m.Handle("GET /api/v1/projects/{id}/ocel", httptransport.NewServer(
		eps.ExportOCEL,
		decodeExportOCELRequest,
		common.EncodeResponse,
		options...,
	))

	// Where a project's running work is sitting now, counted on the server
	// across all of it. The dashboard's heat map reads this.
	m.Handle("GET /api/v1/projects/{id}/waiting", httptransport.NewServer(
		eps.WaitingByStep,
		func(_ context.Context, r *http.Request) (any, error) {
			return process.WaitingByStepRequest{ProjectID: r.PathValue("id")}, nil
		},
		common.EncodeResponse,
		options...,
	))

	// A project's open work with a due date, soonest first, and how much open
	// work it has in all. The dashboard's deadline report reads this.
	m.Handle("GET /api/v1/projects/{id}/deadlines", httptransport.NewServer(
		eps.Deadlines,
		func(_ context.Context, r *http.Request) (any, error) {
			return process.DeadlinesRequest{ProjectID: r.PathValue("id")}, nil
		},
		common.EncodeResponse,
		options...,
	))

	m.Handle("GET /api/v1/instances/{id}/subprocesses", httptransport.NewServer(
		eps.ListSubProcesses,
		decodeListSubProcessesRequest,
		common.EncodeResponse,
		options...,
	))

	m.Handle("POST /api/v1/processes/adhoc/activate", httptransport.NewServer(
		eps.ActivateAdHocTask,
		decodeActivateAdHocTaskRequest,
		common.EncodeResponse,
		options...,
	))

	m.Handle("POST /api/v1/processes/signal", httptransport.NewServer(
		eps.BroadcastSignal,
		decodeBroadcastSignalRequest,
		common.EncodeResponse,
		options...,
	))

	m.Handle("POST /api/v1/processes/message", httptransport.NewServer(
		eps.SendMessage,
		decodeSendMessageRequest,
		common.EncodeResponse,
		options...,
	))

	m.Handle("POST /api/v1/processes/execute-script", httptransport.NewServer(
		eps.ExecuteScript,
		decodeExecuteScriptRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeStartProcessRequest(_ context.Context, r *http.Request) (any, error) {
	var req process.StartProcessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	if req.ProjectID == "" {
		req.ProjectID = r.URL.Query().Get("project_id")
	}
	return req, nil
}

func decodeListInstancesRequest(_ context.Context, r *http.Request) (any, error) {
	// ListInstancesRequest has carried Page and PageSize all along, and the
	// endpoint passes them to ListInstancesPaged — but nothing read them off the
	// query string, so every caller got the first page at the server default and
	// no way past it. A busy project's older instances were unreachable over
	// HTTP while the paging that would reach them was already implemented.
	page, pageSize := common.PageParams(r)
	query := r.URL.Query()
	return process.ListInstancesRequest{
		ProjectID: query.Get("project_id"),
		Page:      page,
		PageSize:  pageSize,
		// Passed through as written. The endpoint refuses a status it does not
		// recognise, which is the behaviour that matters — normalising one here
		// would quietly turn a typo into "every instance in the project".
		Status:       query.Get("status"),
		DefinitionID: query.Get("definition_id"),
		// Presence is not enough: ?needs_attention=false must mean false, or a
		// caller clearing the filter by setting it cannot.
		NeedsAttention: query.Get("needs_attention") == "true",
	}, nil
}

func decodeGetInstanceRequest(_ context.Context, r *http.Request) (any, error) {
	id := r.PathValue("id")
	return process.GetInstanceRequest{ID: id}, nil
}

func decodeGetExecutionPathRequest(_ context.Context, r *http.Request) (any, error) {
	id := r.PathValue("id")
	return process.GetExecutionPathRequest{InstanceID: id}, nil
}

func decodeGetAuditLogsRequest(_ context.Context, r *http.Request) (any, error) {
	id := r.PathValue("id")
	return process.GetAuditLogsRequest{InstanceID: id}, nil
}

func decodeExportOCELRequest(_ context.Context, r *http.Request) (any, error) {
	// Anything other than an explicit "true" means off. A query string is
	// caller-supplied and a permissive parse here — treating "1", "yes" or a
	// bare "?include_variables" as consent — would widen what leaves the
	// building on a typo.
	return process.ExportOCELRequest{
		ProjectID:        r.PathValue("id"),
		IncludeVariables: r.URL.Query().Get("include_variables") == "true",
	}, nil
}

func decodeListSubProcessesRequest(_ context.Context, r *http.Request) (any, error) {
	return process.ListSubProcessesRequest{ParentInstanceID: r.PathValue("id")}, nil
}

func decodeActivateAdHocTaskRequest(_ context.Context, r *http.Request) (any, error) {
	var req process.ActivateAdHocTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeBroadcastSignalRequest(_ context.Context, r *http.Request) (any, error) {
	var req process.BroadcastSignalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeSendMessageRequest(_ context.Context, r *http.Request) (any, error) {
	var req process.SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeExecuteScriptRequest(_ context.Context, r *http.Request) (any, error) {
	var req process.ExecuteScriptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}
