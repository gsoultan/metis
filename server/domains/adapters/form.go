package adapters

import (
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

type FormModelAdapter struct {
	Form entities.Form
}

func (a FormModelAdapter) ToModel() models.FormModel {
	var projectID uuid.UUID
	if a.Form.Project != nil {
		projectID = a.Form.Project.ID
	}
	return models.FormModel{
		Base: models.Base{
			ID:        models.UUID(a.Form.ID),
			CreatedAt: a.Form.CreatedAt,
		},
		ProjectID: models.UUID(projectID),
		Key:       a.Form.Key,
		Name:      a.Form.Name,
		Schema:    a.Form.Schema,
	}
}

type FormEntityAdapter struct {
	Model models.FormModel
}

func (a FormEntityAdapter) ToEntity() entities.Form {
	return entities.Form{
		ID:        uuid.UUID(a.Model.ID),
		Project:   &entities.Project{ID: uuid.UUID(a.Model.ProjectID)},
		Key:       a.Model.Key,
		Name:      a.Model.Name,
		Schema:    a.Model.Schema,
		CreatedAt: a.Model.CreatedAt,
	}
}
