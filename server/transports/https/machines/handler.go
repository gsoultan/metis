// Package machines serves the Servers page: the processes running this
// installation.
package machines

import (
	"context"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"

	"github.com/gsoultan/metis/server/endpoints/machine"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps machine.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/machines", httptransport.NewServer(
		eps.ListMachines, noRequest, common.EncodeResponse, options...))
	m.Handle("DELETE /api/v1/machines/{id}", httptransport.NewServer(
		eps.ForgetMachine, decodeID, common.EncodeResponse, options...))
}

func noRequest(context.Context, *http.Request) (any, error) { return nil, nil }

func decodeID(_ context.Context, r *http.Request) (any, error) {
	return machine.IDRequest{ID: r.PathValue("id")}, nil
}
