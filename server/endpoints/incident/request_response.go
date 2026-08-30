package incident

import (
	"github.com/gsoultan/metis/server/domains/entities"
)

type ListIncidentsRequest struct {
	InstanceID string `json:"instanceId"`
}

type ListIncidentsResponse struct {
	Incidents []entities.Incident `json:"incidents,omitzero"`
	Err       error               `json:"err,omitzero"`
}

func (r ListIncidentsResponse) Failed() error { return r.Err }

type ResolveIncidentRequest struct {
	ID string `json:"id"`
}

type ResolveIncidentResponse struct {
	Err error `json:"err,omitzero"`
}

func (r ResolveIncidentResponse) Failed() error { return r.Err }
