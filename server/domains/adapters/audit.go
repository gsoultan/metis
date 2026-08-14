package adapters

import (
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

type AuditModelAdapter struct {
	Entry entities.AuditEntry
}

func (a AuditModelAdapter) ToModel() models.AuditModel {
	var projectID, instanceID uuid.UUID
	if a.Entry.Project != nil {
		projectID = a.Entry.Project.ID
	}
	if a.Entry.Instance != nil {
		instanceID = a.Entry.Instance.ID
	}
	return models.AuditModel{
		Base: models.Base{
			ID:        models.UUID(a.Entry.ID),
			CreatedAt: a.Entry.Timestamp,
		},
		ProjectID:  models.UUID(projectID),
		InstanceID: models.UUID(instanceID),
		Type:       a.Entry.Type,
		NodeID: func() string {
			if a.Entry.Node != nil {
				return a.Entry.Node.ID
			}
			return ""
		}(),
		NodeName: func() string {
			if a.Entry.Node != nil {
				return a.Entry.Node.Name
			}
			return ""
		}(),
		Message:   a.Entry.Message,
		Narrative: a.Entry.Narrative,
		Data:      a.Entry.Data,
	}
}

type AuditEntityAdapter struct {
	Model models.AuditModel
}

func (a AuditEntityAdapter) ToEntity() entities.AuditEntry {
	return entities.AuditEntry{
		ID:        uuid.UUID(a.Model.ID),
		Project:   &entities.Project{ID: uuid.UUID(a.Model.ProjectID)},
		Instance:  &entities.ProcessInstance{ID: uuid.UUID(a.Model.InstanceID)},
		Type:      a.Model.Type,
		Node:      &entities.Node{ID: a.Model.NodeID, Name: a.Model.NodeName},
		Message:   a.Model.Message,
		Narrative: a.Model.Narrative,
		Data:      a.Model.Data,
		Timestamp: a.Model.CreatedAt,
	}
}
