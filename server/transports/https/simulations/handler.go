// Package simulations exposes simulation on the public REST surface.
//
// REST rather than Connect, deliberately. This is the endpoint the Go SDK
// calls, which means a case somebody clicks through in the designer is the same
// case a pipeline runs — and the "run this in CI" snippet beside every case is
// a fact rather than a guess. Putting simulation only on the first-party
// transport would have made that snippet unverifiable.
package simulations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/metis/server/endpoints/simulation"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// maxBodyBytes bounds a simulation request.
//
// A request carries variables and answers, both authored by the caller, and
// both are read into memory before anything validates them. A megabyte is far
// more than a suite of cases needs and far less than a way to exhaust a server.
const maxBodyBytes = 1 << 20

func RegisterHandlers(m *http.ServeMux, eps simulation.Endpoints, options []httptransport.ServerOption) {
	m.Handle("POST /api/v1/simulations", httptransport.NewServer(
		eps.Simulate,
		decodeSimulateRequest,
		common.EncodeResponse,
		options...,
	))

	m.Handle("POST /api/v1/simulations/batch", httptransport.NewServer(
		eps.SimulateBatch,
		decodeBatchRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeSimulateRequest(_ context.Context, r *http.Request) (any, error) {
	var req simulation.SimulateRequest
	if err := decodeJSON(r, &req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeBatchRequest(_ context.Context, r *http.Request) (any, error) {
	var req simulation.BatchRequest
	if err := decodeJSON(r, &req); err != nil {
		return nil, err
	}
	return req, nil
}

// decodeJSON reads a bounded body and refuses fields nobody declared.
//
// DisallowUnknownFields is the useful half: a caller who sends `stubs` where
// the field is `answers`, or misspells `definition_key`, gets told so rather
// than getting a run of the live definition with no answers — which would look
// like a working request returning a puzzling result.
func decodeJSON(r *http.Request, into any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return fmt.Errorf("this request body could not be read: %w", err)
	}
	return nil
}
