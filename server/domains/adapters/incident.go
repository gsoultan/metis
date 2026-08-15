package adapters

import (
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

type IncidentModelAdapter struct {
	Incident entities.Incident
}

func (a IncidentModelAdapter) ToModel() models.IncidentModel {
	var jobID, instanceID, defID uuid.UUID
	if a.Incident.Job != nil {
		jobID = a.Incident.Job.ID
	}
	if a.Incident.Instance != nil {
		instanceID = a.Incident.Instance.ID
	}
	if a.Incident.Definition != nil {
		defID = a.Incident.Definition.ID
	}
	return models.IncidentModel{
		Base: models.Base{
			ID:        models.UUID(a.Incident.ID),
			CreatedAt: a.Incident.CreatedAt,
		},
		JobID:        models.UUID(jobID),
		InstanceID:   models.UUID(instanceID),
		DefinitionID: models.UUID(defID),
		NodeID: func() string {
			if a.Incident.Node != nil {
				return a.Incident.Node.ID
			}
			return ""
		}(),
		Error:      a.Incident.Error,
		Status:     models.IncidentStatus(a.Incident.Status),
		ResolvedAt: a.Incident.ResolvedAt,
	}
}

type IncidentEntityAdapter struct {
	Model models.IncidentModel
}

func (a IncidentEntityAdapter) ToEntity() entities.Incident {
	return entities.Incident{
		ID:         uuid.UUID(a.Model.ID),
		Job:        &entities.Job{ID: uuid.UUID(a.Model.JobID)},
		Instance:   &entities.ProcessInstance{ID: uuid.UUID(a.Model.InstanceID)},
		Definition: &entities.ProcessDefinition{ID: uuid.UUID(a.Model.DefinitionID)},
		Node:       &entities.Node{ID: a.Model.NodeID},
		Error:      a.Model.Error,
		Status:     entities.IncidentStatus(a.Model.Status),
		CreatedAt:  a.Model.CreatedAt,
		ResolvedAt: a.Model.ResolvedAt,
	}
}
