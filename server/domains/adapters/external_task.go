package adapters

import (
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

type ExternalTaskModelAdapter struct {
	ExternalTask entities.ExternalTask
}

func (a ExternalTaskModelAdapter) ToModel() models.ExternalTaskModel {
	var projectID, instanceID, defID uuid.UUID
	if a.ExternalTask.Project != nil {
		projectID = a.ExternalTask.Project.ID
	}
	if a.ExternalTask.ProcessInstance != nil {
		instanceID = a.ExternalTask.ProcessInstance.ID
	}
	if a.ExternalTask.ProcessDefinition != nil {
		defID = a.ExternalTask.ProcessDefinition.ID
	}
	return models.ExternalTaskModel{
		Base: models.Base{
			ID:        models.UUID(a.ExternalTask.ID),
			CreatedAt: a.ExternalTask.CreatedAt,
		},
		ProjectID:           models.UUID(projectID),
		ProcessInstanceID:   models.UUID(instanceID),
		ProcessDefinitionID: models.UUID(defID),
		NodeID: func() string {
			if a.ExternalTask.Node != nil {
				return a.ExternalTask.Node.ID
			}
			return ""
		}(),
		Topic:          a.ExternalTask.Topic,
		WorkerID:       a.ExternalTask.WorkerID,
		LockExpiration: a.ExternalTask.LockExpiration,
		Retries:        a.ExternalTask.Retries,
		RetryTimeout:   a.ExternalTask.RetryTimeout,
		ErrorMessage:   a.ExternalTask.ErrorMessage,
		ErrorDetails:   a.ExternalTask.ErrorDetails,
		Variables:      a.ExternalTask.Variables,
	}
}

type ExternalTaskEntityAdapter struct {
	Model models.ExternalTaskModel
}

func (a ExternalTaskEntityAdapter) ToEntity() entities.ExternalTask {
	return entities.ExternalTask{
		ID:                uuid.UUID(a.Model.ID),
		Project:           &entities.Project{ID: uuid.UUID(a.Model.ProjectID)},
		ProcessInstance:   &entities.ProcessInstance{ID: uuid.UUID(a.Model.ProcessInstanceID)},
		ProcessDefinition: &entities.ProcessDefinition{ID: uuid.UUID(a.Model.ProcessDefinitionID)},
		Node:              &entities.Node{ID: a.Model.NodeID},
		Topic:             a.Model.Topic,
		WorkerID:          a.Model.WorkerID,
		LockExpiration:    a.Model.LockExpiration,
		Retries:           a.Model.Retries,
		RetryTimeout:      a.Model.RetryTimeout,
		ErrorMessage:      a.Model.ErrorMessage,
		ErrorDetails:      a.Model.ErrorDetails,
		Variables:         a.Model.Variables,
		CreatedAt:         a.Model.CreatedAt,
	}
}
