package adapters

import (
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

type ProjectModelAdapter struct {
	Project entities.Project
}

func (a ProjectModelAdapter) ToModel() models.ProjectModel {
	var orgID uuid.UUID
	if a.Project.Organization != nil {
		orgID = a.Project.Organization.ID
	}
	return models.ProjectModel{
		Base: models.Base{
			ID:        models.UUID(a.Project.ID),
			CreatedAt: a.Project.CreatedAt,
			UpdatedAt: a.Project.UpdatedAt,
		},
		OrganizationID: models.UUID(orgID),
		Name:           a.Project.Name,
		Description:    a.Project.Description,
	}
}

type ProjectEntityAdapter struct {
	Model models.ProjectModel
}

func (a ProjectEntityAdapter) ToEntity() entities.Project {
	return entities.Project{
		ID:           uuid.UUID(a.Model.ID),
		Organization: &entities.Organization{ID: uuid.UUID(a.Model.OrganizationID)},
		Name:         a.Model.Name,
		Description:  a.Model.Description,
		CreatedAt:    a.Model.CreatedAt,
		UpdatedAt:    a.Model.UpdatedAt,
	}
}
