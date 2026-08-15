package processes

import (
	"context"
	"encoding/json"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/gobpm/server/endpoints/process"
	"github.com/gsoultan/gobpm/server/transports/https/common"
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
	return process.ListInstancesRequest{
		ProjectID: r.URL.Query().Get("project_id"),
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
