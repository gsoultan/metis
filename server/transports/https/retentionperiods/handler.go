package retentionperiods

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
	"github.com/gsoultan/metis/server/endpoints/retentionperiod"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// maxPeriodBodyBytes bounds a request to set a period: a number and a reason
// of at most 2,000 characters, each at most four bytes.
const maxPeriodBodyBytes = 16 << 10

func RegisterHandlers(m *http.ServeMux, eps retentionperiod.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/projects/{id}/retention-periods", httptransport.NewServer(
		eps.ListRetentionPeriods,
		decodeListPeriodsRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("PUT /api/v1/projects/{id}/retention-periods/{key}", httptransport.NewServer(
		eps.SetRetentionPeriod,
		decodeSetPeriodRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/projects/{id}/retention-periods/{key}/history", httptransport.NewServer(
		eps.RetentionPeriodHistory,
		decodeListPeriodsRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeListPeriodsRequest(_ context.Context, r *http.Request) (any, error) {
	return retentionperiod.ListPeriodsRequest{ProjectID: r.PathValue("id"), ProcessKey: r.PathValue("key")}, nil
}

// periodBody is the body as sent. Days is kept raw so a body that leaves it
// out is told apart from one that sends null: leaving it out must not return
// a process to the installation's setting by accident.
type periodBody struct {
	Days   json.RawMessage `json:"days"`
	Reason string          `json:"reason"`
}

// decodeSetPeriodRequest reads {"days": n or null, "reason": "..."} and
// nothing else: one JSON object, no other field, nothing after it.
func decodeSetPeriodRequest(_ context.Context, r *http.Request) (any, error) {
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxPeriodBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, apierr.Invalidf("this request is larger than %d KiB, which is more than a period and a reason need", maxPeriodBodyBytes>>10)
		}
		return nil, unreadablePeriodRequest(r, err)
	}
	var sent periodBody
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&sent); err != nil {
		return nil, unreadablePeriodRequest(r, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, unreadablePeriodRequest(r, errors.New("something follows the JSON object"))
	}
	if len(sent.Days) == 0 {
		return nil, apierr.Invalidf(`say how long: "days" is a whole number of days, or null to follow the installation's setting`)
	}
	req := retentionperiod.SetPeriodRequest{ProjectID: r.PathValue("id"), ProcessKey: r.PathValue("key"), Reason: sent.Reason}
	if err := json.Unmarshal(sent.Days, &req.Days); err != nil {
		return nil, apierr.Invalidf(`"days" is a whole number of days, or null to follow the installation's setting`)
	}
	return req, nil
}

// unreadablePeriodRequest is the refusal of a body that could not be read, in
// words somebody can act on; why is logged, not told.
func unreadablePeriodRequest(r *http.Request, why error) error {
	log.Debug().Err(why).Str("path", r.URL.Path).Msg("A request to set a retention period could not be read")
	return apierr.Invalidf(`this request could not be read: send one JSON object, {"days": 365, "reason": "why"}`)
}
