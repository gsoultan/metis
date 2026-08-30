package incidents

import (
	"context"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/metis/server/endpoints/incident"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func RegisterHandlers(m *http.ServeMux, eps incident.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/incidents/{instanceId}", httptransport.NewServer(
		eps.ListIncidents,
		decodeListIncidentsRequest,
		common.EncodeResponse,
		options...,
	))

	m.Handle("POST /api/v1/incidents/{id}/resolve", httptransport.NewServer(
		eps.ResolveIncident,
		decodeResolveIncidentRequest,
		common.EncodeResponse,
		options...,
	))
}

func decodeListIncidentsRequest(_ context.Context, r *http.Request) (any, error) {
	instanceId := r.PathValue("instanceId")
	return incident.ListIncidentsRequest{InstanceID: instanceId}, nil
}

func decodeResolveIncidentRequest(_ context.Context, r *http.Request) (any, error) {
	id := r.PathValue("id")
	return incident.ResolveIncidentRequest{ID: id}, nil
}
