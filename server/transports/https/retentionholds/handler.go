package retentionholds

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
	"github.com/gsoultan/metis/server/endpoints/retentionhold"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// maxHoldBodyBytes bounds a request to place or release a hold. It carries a
// reason of at most 2,000 characters, each at most four bytes; this is room
// for that and far less than a way to exhaust a server.
const maxHoldBodyBytes = 16 << 10

func RegisterHandlers(m *http.ServeMux, eps retentionhold.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/instances/{id}/retention-hold", httptransport.NewServer(
		eps.GetInstanceHold,
		decodeInstanceHoldRequest,
		common.EncodeResponse,
		options...,
	))
	// Place and release are separate posts rather than a PUT and a DELETE:
	// each carries a reason, and a DELETE's body is one some proxies drop.
	m.Handle("POST /api/v1/instances/{id}/retention-hold", httptransport.NewServer(
		eps.HoldInstance,
		decodeChangeHoldRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/instances/{id}/retention-hold/release", httptransport.NewServer(
		eps.ReleaseInstanceHold,
		decodeChangeHoldRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/projects/{id}/retention-holds", httptransport.NewServer(
		eps.ListRetentionHolds,
		decodeListHoldsRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeInstanceHoldRequest(_ context.Context, r *http.Request) (any, error) {
	return retentionhold.InstanceHoldRequest{InstanceID: r.PathValue("id")}, nil
}

func decodeListHoldsRequest(_ context.Context, r *http.Request) (any, error) {
	return retentionhold.ListHoldsRequest{ProjectID: r.PathValue("id")}, nil
}

// decodeChangeHoldRequest reads {"reason": "..."} and nothing else: one JSON
// object, no field but reason, nothing after it.
func decodeChangeHoldRequest(_ context.Context, r *http.Request) (any, error) {
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxHoldBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, apierr.Invalidf("this request is larger than %d KiB, which is more than a reason needs", maxHoldBodyBytes>>10)
		}
		return nil, unreadableHoldRequest(r, err)
	}
	var req retentionhold.ChangeHoldRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return nil, unreadableHoldRequest(r, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, unreadableHoldRequest(r, errors.New("something follows the JSON object"))
	}
	req.InstanceID = r.PathValue("id")
	return req, nil
}

// unreadableHoldRequest is the refusal of a body that could not be read, in
// words somebody can act on; why is logged, not told.
func unreadableHoldRequest(r *http.Request, why error) error {
	log.Debug().Err(why).Str("path", r.URL.Path).Msg("A request to place or release a retention hold could not be read")
	return apierr.Invalidf(`this request could not be read: send one JSON object, {"reason": "why"}`)
}
