// Package external_tasks exposes the external-task worker protocol over HTTP.
//
// This is the integration surface for work that cannot or should not run inside
// the engine: heavy compute, private-network access, another language's SDK. A
// worker long-polls fetch-and-lock for its topic, does the work in its own
// runtime, and reports back — the pull model, so the engine never needs to
// reach into the worker's network.
//
// Until now this protocol existed only over gRPC and the AMQP bridge, which
// made "write a worker in anything that speaks HTTP" impossible — the one
// property a worker protocol exists to provide.
package external_tasks

import (
	"context"
	"encoding/json"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/endpoints/external_task"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// defaultLockDurationMS is used when a fetch request does not say how long it
// needs. Long enough for real work, short enough that a crashed worker's tasks
// come back within a minute.
const defaultLockDurationMS = 60_000

// RegisterHandlers mounts the worker protocol routes.
func RegisterHandlers(m *http.ServeMux, eps external_task.Endpoints, options []httptransport.ServerOption) {
	m.Handle("POST /api/v1/external-tasks/fetch-and-lock", httptransport.NewServer(
		eps.FetchAndLockExternal,
		decodeFetchAndLockRequest,
		common.EncodeResponse,
		options...,
	))

	m.Handle("POST /api/v1/external-tasks/{id}/complete", httptransport.NewServer(
		eps.CompleteExternal,
		decodeCompleteRequest,
		common.EncodeResponse,
		options...,
	))

	m.Handle("POST /api/v1/external-tasks/{id}/failure", httptransport.NewServer(
		eps.HandleExternalFailure,
		decodeFailureRequest,
		common.EncodeResponse,
		options...,
	))
}

// fetchAndLockBody is the wire shape. It exists so the JSON contract is
// snake_case and explicit about units, rather than leaking the endpoint
// struct's untagged field names into a public API.
type fetchAndLockBody struct {
	Topic    string `json:"topic"`
	WorkerID string `json:"worker_id"`
	MaxTasks int    `json:"max_tasks"`
	// LockDurationMS is how long the returned tasks stay invisible to other
	// workers, in milliseconds. Named with its unit because a bare
	// "lock_duration" was already misread once inside this codebase — the AMQP
	// bridge passed 30 meaning seconds and got 30 milliseconds.
	LockDurationMS int64 `json:"lock_duration_ms"`
}

func decodeFetchAndLockRequest(_ context.Context, r *http.Request) (any, error) {
	var body fetchAndLockBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.MaxTasks <= 0 {
		body.MaxTasks = 1
	}
	if body.LockDurationMS <= 0 {
		body.LockDurationMS = defaultLockDurationMS
	}
	return external_task.FetchAndLockExternalRequest{
		Topic:        body.Topic,
		WorkerID:     body.WorkerID,
		MaxTasks:     body.MaxTasks,
		LockDuration: body.LockDurationMS,
	}, nil
}

type completeBody struct {
	WorkerID  string         `json:"worker_id"`
	Variables map[string]any `json:"variables,omitzero"`
}

func decodeCompleteRequest(_ context.Context, r *http.Request) (any, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	var body completeBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, err
	}
	return external_task.CompleteExternalRequest{
		TaskID:    id,
		WorkerID:  body.WorkerID,
		Variables: body.Variables,
	}, nil
}

type failureBody struct {
	WorkerID     string `json:"worker_id"`
	ErrorMessage string `json:"error_message"`
	ErrorDetails string `json:"error_details,omitzero"`
	// Retries is how many attempts remain after this failure. Zero means give
	// up: the task stays failed for an operator rather than being retried.
	Retries int `json:"retries"`
	// RetryTimeoutMS is how long to wait before the task becomes fetchable
	// again, in milliseconds.
	RetryTimeoutMS int64 `json:"retry_timeout_ms"`
}

func decodeFailureRequest(_ context.Context, r *http.Request) (any, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	var body failureBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, err
	}
	return external_task.HandleExternalFailureRequest{
		TaskID:       id,
		WorkerID:     body.WorkerID,
		ErrorMessage: body.ErrorMessage,
		ErrorDetails: body.ErrorDetails,
		Retries:      body.Retries,
		RetryTimeout: body.RetryTimeoutMS,
	}, nil
}
