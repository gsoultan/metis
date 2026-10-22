package batchoperations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/endpoints/batchoperation"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// maxBatchBodyBytes bounds a request to start or stop an operation. It
// carries a reason of at most 2,000 characters and a filter of a few short
// fields; this is room for that and far less than a way to exhaust a server.
const maxBatchBodyBytes = 16 << 10

func RegisterHandlers(m *http.ServeMux, eps batchoperation.Endpoints, options []httptransport.ServerOption) {
	m.Handle("POST /api/v1/projects/{id}/batch-operations", httptransport.NewServer(
		eps.StartBatchOperation,
		decodeStartRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/projects/{id}/batch-operations", httptransport.NewServer(
		eps.ListBatchOperations,
		func(_ context.Context, r *http.Request) (any, error) {
			return batchoperation.ListRequest{ProjectID: r.PathValue("id")}, nil
		},
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/batch-operations/{id}", httptransport.NewServer(
		eps.GetBatchOperation,
		func(_ context.Context, r *http.Request) (any, error) {
			return batchoperation.OperationRequest{ID: r.PathValue("id")}, nil
		},
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/batch-operations/{id}/items", httptransport.NewServer(
		eps.BatchOperationItems,
		decodeItemsRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/batch-operations/{id}/stop", httptransport.NewServer(
		eps.StopBatchOperation,
		decodeStopRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeStartRequest(_ context.Context, r *http.Request) (any, error) {
	var req batchoperation.StartRequest
	if err := decodeStrict(r, &req, `{"kind": "retry", "reason": "why", "filter": {...}, "dry_run": false}`); err != nil {
		return nil, err
	}
	req.ProjectID = r.PathValue("id")
	return req, nil
}

func decodeStopRequest(_ context.Context, r *http.Request) (any, error) {
	var req batchoperation.StopRequest
	if err := decodeStrict(r, &req, `{"reason": "why"}`); err != nil {
		return nil, err
	}
	req.ID = r.PathValue("id")
	return req, nil
}

func decodeItemsRequest(_ context.Context, r *http.Request) (any, error) {
	page, pageSize := common.PageParams(r)
	return batchoperation.ItemsRequest{
		ID:       r.PathValue("id"),
		Status:   r.URL.Query().Get("status"),
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// decodeStrict reads one JSON object with no field the request does not
// have, and nothing after it.
func decodeStrict(r *http.Request, into any, shape string) error {
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxBatchBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return apierr.Invalidf("this request is larger than %d KiB, which is more than a batch operation needs", maxBatchBodyBytes>>10)
		}
		return unreadable(r, err, shape)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return unreadable(r, err, shape)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return unreadable(r, errors.New("something follows the JSON object"), shape)
	}
	return nil
}

// unreadable is the refusal of a body that could not be read, in words
// somebody can act on; why is logged, not told.
func unreadable(r *http.Request, why error, shape string) error {
	log.Debug().Err(why).Str("path", r.URL.Path).Msg("A batch operation request could not be read")
	return apierr.Invalidf("this request could not be read: send one JSON object, %s", shape)
}
