package adapters

import (
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

type ConnectorInstanceModelAdapter struct {
	Instance entities.ConnectorInstance
}

func (a ConnectorInstanceModelAdapter) ToModel() models.ConnectorInstance {
	var projectID, connectorID uuid.UUID
	if a.Instance.Project != nil {
		projectID = a.Instance.Project.ID
	}
	if a.Instance.Connector != nil {
		connectorID = a.Instance.Connector.ID
	}
	return models.ConnectorInstance{
		Base: models.Base{
			ID:        models.UUID(a.Instance.ID),
			CreatedAt: a.Instance.CreatedAt,
			UpdatedAt: a.Instance.UpdatedAt,
		},
		ProjectID:   models.UUID(projectID),
		ConnectorID: models.UUID(connectorID),
		Name:        a.Instance.Name,
		Config:      a.Instance.Config,
	}
}

type ConnectorInstanceEntityAdapter struct {
	Model models.ConnectorInstance
}

func (a ConnectorInstanceEntityAdapter) ToEntity() entities.ConnectorInstance {
	return entities.ConnectorInstance{
		ID:        uuid.UUID(a.Model.ID),
		Project:   &entities.Project{ID: uuid.UUID(a.Model.ProjectID)},
		Connector: &entities.Connector{ID: uuid.UUID(a.Model.ConnectorID)},
		Name:      a.Model.Name,
		Config:    a.Model.Config,
		CreatedAt: a.Model.CreatedAt,
		UpdatedAt: a.Model.UpdatedAt,
	}
}
