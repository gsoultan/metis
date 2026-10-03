package deviations

import (
	"context"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/metis/server/endpoints/deviation"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps deviation.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/instances/{id}/deviations", httptransport.NewServer(
		eps.ListInstanceDeviations,
		decodeListInstanceDeviationsRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeListInstanceDeviationsRequest(_ context.Context, r *http.Request) (any, error) {
	return deviation.ListInstanceDeviationsRequest{InstanceID: r.PathValue("id")}, nil
}
