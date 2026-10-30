package archives

import (
	"context"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"

	"github.com/gsoultan/metis/server/endpoints/archive"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps archive.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/projects/{id}/archived-instances", httptransport.NewServer(
		eps.ListArchivedInstances,
		decodeListArchivedRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/archived-instances/{id}", httptransport.NewServer(
		eps.GetArchivedInstance,
		decodeGetArchivedRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeListArchivedRequest(_ context.Context, r *http.Request) (any, error) {
	query := r.URL.Query()
	return archive.ListArchivedRequest{
		ProjectID:   r.PathValue("id"),
		ProcessKey:  query.Get("process_key"),
		BusinessKey: query.Get("business_key"),
		Cursor:      query.Get("cursor"),
		Limit:       query.Get("limit"),
	}, nil
}

func decodeGetArchivedRequest(_ context.Context, r *http.Request) (any, error) {
	return archive.GetArchivedRequest{InstanceID: r.PathValue("id")}, nil
}
