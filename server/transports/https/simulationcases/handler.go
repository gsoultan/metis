// Package simulationcases exposes a process's saved simulation test cases on
// the REST surface, beside the simulation endpoint that runs them.
package simulationcases

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/endpoints/simulationcase"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// maxCaseBodyBytes bounds a save. A case keeps at most 64 KiB once encoded;
// the body is allowed the same and a little for its name and punctuation.
const maxCaseBodyBytes = 80 << 10

func RegisterHandlers(m *http.ServeMux, eps simulationcase.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/projects/{id}/simulation-cases/{key}", httptransport.NewServer(
		eps.ListSimulationCases, decodeListRequest, common.EncodeResponse, options...,
	))
	m.Handle("POST /api/v1/projects/{id}/simulation-cases/{key}", httptransport.NewServer(
		eps.SaveSimulationCase, decodeSaveRequest, common.EncodeResponse, options...,
	))
	m.Handle("PUT /api/v1/projects/{id}/simulation-cases/{key}/{case}", httptransport.NewServer(
		eps.SaveSimulationCase, decodeSaveRequest, common.EncodeResponse, options...,
	))
	m.Handle("DELETE /api/v1/projects/{id}/simulation-cases/{key}/{case}", httptransport.NewServer(
		eps.DeleteSimulationCase, decodeDeleteRequest, common.EncodeResponse, options...,
	))
}

func decodeListRequest(_ context.Context, r *http.Request) (any, error) {
	return simulationcase.ListCasesRequest{ProjectID: r.PathValue("id"), ProcessKey: r.PathValue("key")}, nil
}

func decodeDeleteRequest(_ context.Context, r *http.Request) (any, error) {
	return simulationcase.DeleteCaseRequest{CaseID: r.PathValue("case")}, nil
}

// decodeSaveRequest reads one JSON object and refuses fields nobody declared,
// for the reason the simulation endpoint gives: a misspelt "expect" would
// otherwise save a case that checks nothing and passes every run.
func decodeSaveRequest(_ context.Context, r *http.Request) (any, error) {
	var req simulationcase.SaveCaseRequest
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxCaseBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, apierr.Invalidf("this case is larger than %d KiB, which is more than a case keeps", maxCaseBodyBytes>>10)
		}
		return nil, apierr.Invalidf("this case could not be read: %v", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, apierr.Invalidf("this case could not be read: something follows the JSON object")
	}
	req.ProjectID = r.PathValue("id")
	req.ProcessKey = r.PathValue("key")
	req.CaseID = r.PathValue("case")
	return req, nil
}
