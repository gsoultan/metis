// Package approvals serves approval lines over HTTP.
package approvals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/endpoints/approval"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// maxBodyBytes bounds a line or a request's variables. A line is at most
// twenty steps; a request's variables are a form's worth of fields.
const maxBodyBytes = 1 << 20

func RegisterHandlers(m *http.ServeMux, eps approval.Endpoints, options []httptransport.ServerOption) {
	m.Handle("POST /api/v1/projects/{id}/approval-lines", httptransport.NewServer(
		eps.DefineApprovalLine, decodeDefine, common.EncodeResponse, options...))
	m.Handle("GET /api/v1/projects/{id}/approval-lines/{key}", httptransport.NewServer(
		eps.GetApprovalLine, decodeGetLine, common.EncodeResponse, options...))
	m.Handle("POST /api/v1/projects/{id}/approval-lines/{key}/requests", httptransport.NewServer(
		eps.SubmitApproval, decodeSubmit, common.EncodeResponse, options...))
	m.Handle("GET /api/v1/projects/{id}/approval-lines/{key}/requests", httptransport.NewServer(
		eps.ListApprovalRequests, decodeListRequests, common.EncodeResponse, options...))
	m.Handle("GET /api/v1/approval-requests/{id}", httptransport.NewServer(
		eps.GetApprovalRequest, decodeGetRequest, common.EncodeResponse, options...))
	m.Handle("POST /api/v1/approval-tasks/{id}/decision", httptransport.NewServer(
		eps.DecideApproval, decodeDecide, common.EncodeResponse, options...))
}

func decodeDefine(_ context.Context, r *http.Request) (any, error) {
	var line entities.ApprovalLine
	if err := readJSON(r, &line); err != nil {
		return nil, err
	}
	return approval.DefineLineRequest{ProjectID: r.PathValue("id"), Line: line}, nil
}

func decodeGetLine(_ context.Context, r *http.Request) (any, error) {
	return approval.GetLineRequest{ProjectID: r.PathValue("id"), Key: r.PathValue("key")}, nil
}

func decodeSubmit(_ context.Context, r *http.Request) (any, error) {
	req := approval.SubmitRequest{}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	req.ProjectID, req.Key = r.PathValue("id"), r.PathValue("key")
	return req, nil
}

func decodeListRequests(_ context.Context, r *http.Request) (any, error) {
	return approval.ListRequestsRequest{ProjectID: r.PathValue("id"), Key: r.PathValue("key"), SubmittedBy: r.URL.Query().Get("submitted_by")}, nil
}

func decodeGetRequest(_ context.Context, r *http.Request) (any, error) {
	return approval.GetRequestRequest{InstanceID: r.PathValue("id")}, nil
}

func decodeDecide(_ context.Context, r *http.Request) (any, error) {
	req := approval.DecideRequest{}
	if err := readJSON(r, &req); err != nil {
		return nil, err
	}
	req.TaskID = r.PathValue("id")
	return req, nil
}

// readJSON reads one JSON object with no fields it does not know: a
// misspelt field is refused rather than ignored, because an ignored "groups"
// is a step offered to nobody.
func readJSON(r *http.Request, into any) error {
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return apierr.Invalidf("this request is larger than %d KiB", maxBodyBytes>>10)
		}
		return apierr.Invalidf("the request could not be read: %v", err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return apierr.Invalidf("the request is not the JSON expected: %v", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return apierr.Invalidf("something follows the JSON object")
	}
	return nil
}
