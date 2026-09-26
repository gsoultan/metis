package roles

import (
	"context"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/metis/server/endpoints/role"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// RegisterHandlers mounts the role legend: what each role is required for,
// read from the gates. Read-only; who may read it is decided where the
// endpoint is built.
func RegisterHandlers(m *http.ServeMux, eps role.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/roles", httptransport.NewServer(
		eps.ListRoles,
		decodeListRolesRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeListRolesRequest(context.Context, *http.Request) (any, error) {
	return nil, nil
}
