// Package operationlog serves the audit log over HTTP.
package operationlog

import (
	"context"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"

	"github.com/gsoultan/metis/server/endpoints/operationlog"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps operationlog.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/audit/operations", httptransport.NewServer(
		eps.ListOperations,
		decodeListRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeListRequest(_ context.Context, r *http.Request) (any, error) {
	q := r.URL.Query()
	return operationlog.ListRequest{
		Actor:     q.Get("actor"),
		Type:      q.Get("type"),
		ProjectID: q.Get("project"),
		From:      q.Get("from"),
		To:        q.Get("to"),
		Before:    q.Get("before"),
		Limit:     q.Get("limit"),
	}, nil
}
