package adapters

import (
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

type JobModelAdapter struct {
	Job entities.Job
}

func (a JobModelAdapter) ToModel() models.JobModel {
	var instanceID, defID uuid.UUID
	if a.Job.Instance != nil {
		instanceID = a.Job.Instance.ID
	}
	if a.Job.Definition != nil {
		defID = a.Job.Definition.ID
	}
	return models.JobModel{
		Base: models.Base{
			ID:        models.UUID(a.Job.ID),
			CreatedAt: a.Job.CreatedAt,
			UpdatedAt: a.Job.UpdatedAt,
		},
		InstanceID:   models.UUID(instanceID),
		DefinitionID: models.UUID(defID),
		NodeID: func() string {
			if a.Job.Node != nil {
				return a.Job.Node.ID
			}
			return ""
		}(),
		IterationID: a.Job.IterationID,
		Type:        models.JobType(a.Job.Type),
		Status:      models.JobStatus(a.Job.Status),
		Payload:     a.Job.Payload,
		Retries:     a.Job.Retries,
		MaxRetries:  a.Job.MaxRetries,
		NextRunAt:   a.Job.NextRunAt,
		LastError:   a.Job.LastError,
	}
}

type JobEntityAdapter struct {
	Model models.JobModel
}

func (a JobEntityAdapter) ToEntity() entities.Job {
	return entities.Job{
		ID:          uuid.UUID(a.Model.ID),
		Instance:    &entities.ProcessInstance{ID: uuid.UUID(a.Model.InstanceID)},
		Definition:  &entities.ProcessDefinition{ID: uuid.UUID(a.Model.DefinitionID)},
		Node:        &entities.Node{ID: a.Model.NodeID},
		IterationID: a.Model.IterationID,
		Type:        entities.JobType(a.Model.Type),
		Status:      entities.JobStatus(a.Model.Status),
		Payload:     a.Model.Payload,
		Retries:     a.Model.Retries,
		MaxRetries:  a.Model.MaxRetries,
		NextRunAt:   a.Model.NextRunAt,
		CreatedAt:   a.Model.CreatedAt,
		UpdatedAt:   a.Model.UpdatedAt,
		LastError:   a.Model.LastError,
	}
}
