package people

import (
	"context"
	"encoding/json"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/endpoints/people"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// maxDeviceBytes bounds a device registration: a token of up to 4096
// characters, a platform and a name.
const maxDeviceBytes = 8 << 10

// registerDeviceHandlers routes a person's push devices.
func registerDeviceHandlers(m *http.ServeMux, eps people.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/people/devices", httptransport.NewServer(
		eps.ListDevices,
		func(context.Context, *http.Request) (any, error) { return people.DevicesRequest{}, nil },
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/people/devices", httptransport.NewServer(
		eps.RegisterDevice, decodeRegisterDeviceRequest, common.EncodeResponse, options...,
	))
	m.Handle("DELETE /api/v1/people/devices/{id}", httptransport.NewServer(
		eps.RemoveDevice,
		func(_ context.Context, r *http.Request) (any, error) {
			return people.RemoveDeviceRequest{ID: r.PathValue("id")}, nil
		},
		common.EncodeResponse,
		options...,
	))
}

func decodeRegisterDeviceRequest(_ context.Context, r *http.Request) (any, error) {
	var req people.RegisterDeviceRequest
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxDeviceBytes)).Decode(&req); err != nil {
		return nil, apierr.Invalidf("the body must be JSON with the device's token and platform")
	}
	return req, nil
}
