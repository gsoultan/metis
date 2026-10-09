package deviations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/endpoints/deviation"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// maxDecideBodyBytes bounds an approval or a rejection. It says a reason of
// at most 2,000 characters and nothing else, so this is twice what the
// longest reason weighs in the widest characters, and no more.
const maxDecideBodyBytes = 16 << 10

// decideFields is the one field a decision may carry.
//
// It is the whole of what an approver can say. What an approval carries out
// is the command the request sealed when it was asked for; a body that names
// a command, outputs, a visit key or anything else is refused by this set,
// not read past.
var decideFields = map[string]struct{}{"reason": {}}

// registerRequestHandlers routes the requests that wait for a second
// administrator: the queue, one request, and the two decisions. Each reply is
// a Failer, so a refusal is answered with the status of its class and never
// with a 200 that carries an error.
func registerRequestHandlers(m *http.ServeMux, eps deviation.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/deviation-requests", httptransport.NewServer(
		eps.ListDeviationRequests,
		decodeListDeviationRequestsRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/deviation-requests/{id}", httptransport.NewServer(
		eps.GetDeviationRequest,
		decodeGetDeviationRequestRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/deviation-requests/{id}/approve", httptransport.NewServer(
		eps.ApproveDeviationRequest,
		decodeDecideRequest,
		common.EncodeResponse,
		options...,
	))
	// A rejection by whoever asked is a withdrawal; there is no route of its
	// own for one.
	m.Handle("POST /api/v1/deviation-requests/{id}/reject", httptransport.NewServer(
		eps.RejectDeviationRequest,
		decodeDecideRequest,
		common.EncodeResponse,
		options...,
	))
}

// decodeListDeviationRequestsRequest reads what the queue is asked for from
// the address: a status, a project, a page.
//
// A page or a page size that is not a whole number is refused, where the
// neighbouring lists read it as the first page: whoever asked for page "two"
// of a queue of decisions is better told than shown page one. One that is
// left out or empty is not said, and the service's own default stands; one
// below the first page, or a size beyond the largest the server gives, is
// bounded where the page is read; and one past maxQueuePage is refused.
func decodeListDeviationRequestsRequest(_ context.Context, r *http.Request) (any, error) {
	query := r.URL.Query()
	page, err := wholeNumber(query.Get("page"))
	if err != nil {
		return nil, err
	}
	pageSize, err := wholeNumber(query.Get("page_size"))
	if err != nil {
		return nil, err
	}
	return deviation.ListDeviationRequestsRequest{
		Status: query.Get("status"), ProjectID: query.Get("project_id"), Page: page, PageSize: pageSize,
	}, nil
}

// maxQueuePage bounds a page number and a page size as they are asked for.
//
// Where a page starts is the one multiplied by the other. The size is cut to
// the largest page the server gives before that; the page number is not, and
// one near the largest whole number wraps the product round to before the
// first row — the first page, answered as the page asked for. Nobody has a
// millionth page of decisions to read.
const maxQueuePage = 1_000_000

// wholeNumber reads a page or a page size: 0 for one that was not said.
func wholeNumber(said string) (int, error) {
	said = strings.TrimSpace(said)
	if said == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(said)
	if err != nil {
		return 0, apierr.Invalidf("page and page_size are whole numbers")
	}
	if n > maxQueuePage {
		return 0, apierr.Invalidf("page and page_size are at most %d", maxQueuePage)
	}
	return n, nil
}

func decodeGetDeviationRequestRequest(_ context.Context, r *http.Request) (any, error) {
	return deviation.GetDeviationRequestRequest{ID: r.PathValue("id")}, nil
}

// decodeDecideRequest reads an approval or a rejection, and refuses one it
// cannot read exactly.
//
// A decision changes an instance, so it is read as strictly as the request
// that asked for the change (decodeDeviateInstanceRequest): one JSON object
// and nothing after it, the one field a decision has, named exactly and once,
// holding text. Or nothing at all: an approval needs no note, so a body that
// is empty or only white space is a decision that gives no reason — which a
// rejection is then refused for, by the service, in its own words.
//
// The refusal is one sentence and not the decoder's error, which names the
// server's own types; what the decoder said is logged at a level that is off
// unless somebody is looking.
func decodeDecideRequest(_ context.Context, r *http.Request) (any, error) {
	req := deviation.DecideDeviationRequestRequest{ID: r.PathValue("id")}
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxDecideBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, apierr.Invalidf("this request is larger than %d KiB, which is more than a decision needs to say", maxDecideBodyBytes>>10)
		}
		return nil, unreadableDecision(r, err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return req, nil
	}
	if err := namedExactlyAndOnce(body, decideFields); err != nil {
		return nil, unreadableDecision(r, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return nil, unreadableDecision(r, err)
	}
	return req, nil
}

// unreadableDecision is the refusal of a decision that could not be read, in
// words somebody can act on; why is logged, not told.
func unreadableDecision(r *http.Request, why error) error {
	log.Debug().Err(why).Str("path", r.URL.Path).Msg("A decision on a request for a second administrator could not be read")
	return apierr.Invalidf("this request could not be read: send one JSON object with a reason, or nothing; name the field exactly and once")
}
