package adapters

import (
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// CompensatableActivityModelAdapter converts a domain entity to a GORM model.
type CompensatableActivityModelAdapter struct {
	Activity entities.CompensatableActivity
}

func (a CompensatableActivityModelAdapter) ToModel() models.CompensatableActivityModel {
	var instanceID uuid.UUID
	if a.Activity.Instance != nil {
		instanceID = a.Activity.Instance.ID
	}
	m := models.CompensatableActivityModel{
		InstanceID: models.UUID(instanceID),
		NodeID: func() string {
			if a.Activity.Node != nil {
				return a.Activity.Node.ID
			}
			return ""
		}(),
		CompensationNodeID: func() string {
			if a.Activity.CompensationNode != nil {
				return a.Activity.CompensationNode.ID
			}
			return ""
		}(),
		Variables:   a.Activity.Variables,
		CompletedAt: a.Activity.CompletedAt,
		Compensated: a.Activity.Compensated,
	}
	m.ID = models.UUID(a.Activity.ID)
	return m
}

// CompensatableActivityEntityAdapter converts a GORM model to a domain entity.
type CompensatableActivityEntityAdapter struct {
	Model models.CompensatableActivityModel
}

func (a CompensatableActivityEntityAdapter) ToEntity() entities.CompensatableActivity {
	return entities.CompensatableActivity{
		ID:               uuid.UUID(a.Model.ID),
		Instance:         &entities.ProcessInstance{ID: uuid.UUID(a.Model.InstanceID)},
		Node:             &entities.Node{ID: a.Model.NodeID},
		CompensationNode: &entities.Node{ID: a.Model.CompensationNodeID},
		Variables:        a.Model.Variables,
		CompletedAt:      a.Model.CompletedAt,
		Compensated:      a.Model.Compensated,
	}
}
