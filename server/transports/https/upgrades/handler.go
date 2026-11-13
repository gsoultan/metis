package upgrades

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/endpoints/upgrade"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// maxStartBodyBytes bounds a request to start an upgrade: two release tags.
const maxStartBodyBytes = 1 << 10

func noRequest(context.Context, *http.Request) (any, error) { return nil, nil }

func RegisterHandlers(m *http.ServeMux, eps upgrade.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/upgrades", httptransport.NewServer(eps.UpgradeStatus, noRequest, common.EncodeResponse, options...))
	m.Handle("POST /api/v1/upgrades/check", httptransport.NewServer(eps.CheckForUpgrade, noRequest, common.EncodeResponse, options...))
	m.Handle("POST /api/v1/upgrades/start", httptransport.NewServer(eps.StartUpgrade, decodeStart, common.EncodeResponse, options...))
}

// decodeStart reads {"to": "vX.Y.Z", "confirm": "vX.Y.Z"} and nothing else.
func decodeStart(_ context.Context, r *http.Request) (any, error) {
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxStartBodyBytes))
	if err != nil {
		return nil, apierr.Invalidf("an upgrade request is a release and its confirmation, and this one could not be read")
	}
	var start contracts.UpgradeStart
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&start); err != nil {
		return nil, apierr.Invalidf(`send {"to": "<release>", "confirm": "<the same release>"}`)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, apierr.Invalidf(`send {"to": "<release>", "confirm": "<the same release>"} and nothing after it`)
	}
	return start, nil
}
