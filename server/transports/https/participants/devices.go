package participants

import (
	"context"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"

	"github.com/gsoultan/metis/server/endpoints/participant"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// registerDeviceHandlers routes an administrator's view of People's push
// devices. The project always comes from the query, so the service can check
// the person is in it.
func registerDeviceHandlers(m *http.ServeMux, eps participant.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/participants/device-counts", httptransport.NewServer(
		eps.DeviceCounts,
		func(_ context.Context, r *http.Request) (any, error) {
			return participant.DeviceCountsRequest{ProjectID: r.URL.Query().Get("project_id")}, nil
		},
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/participants/{id}/devices", httptransport.NewServer(
		eps.ListDevices,
		func(_ context.Context, r *http.Request) (any, error) {
			return participant.ParticipantDevicesRequest{ProjectID: r.URL.Query().Get("project_id"), ID: r.PathValue("id")}, nil
		},
		common.EncodeResponse,
		options...,
	))
	m.Handle("DELETE /api/v1/participants/{id}/devices/{device}", httptransport.NewServer(
		eps.RemoveDevice,
		func(_ context.Context, r *http.Request) (any, error) {
			return participant.RemoveParticipantDeviceRequest{
				ProjectID: r.URL.Query().Get("project_id"), ID: r.PathValue("id"), DeviceID: r.PathValue("device"),
			}, nil
		},
		common.EncodeResponse,
		options...,
	))
}
