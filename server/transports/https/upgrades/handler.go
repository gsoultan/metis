package upgrades

import (
	"context"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"

	"github.com/gsoultan/metis/server/endpoints/upgrade"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func noRequest(context.Context, *http.Request) (any, error) { return nil, nil }

func RegisterHandlers(m *http.ServeMux, eps upgrade.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/upgrades", httptransport.NewServer(eps.UpgradeStatus, noRequest, common.EncodeResponse, options...))
	m.Handle("POST /api/v1/upgrades/check", httptransport.NewServer(eps.CheckForUpgrade, noRequest, common.EncodeResponse, options...))
}
