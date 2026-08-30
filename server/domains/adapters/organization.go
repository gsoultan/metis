package adapters

import (
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

type OrganizationModelAdapter struct {
	Organization entities.Organization
}

func (a OrganizationModelAdapter) ToModel() models.OrganizationModel {
	return models.OrganizationModel{
		Base: models.Base{
			ID:        models.UUID(a.Organization.ID),
			CreatedAt: a.Organization.CreatedAt,
			UpdatedAt: a.Organization.UpdatedAt,
		},
		Name:        a.Organization.Name,
		Description: a.Organization.Description,
	}
}

type OrganizationEntityAdapter struct {
	Model models.OrganizationModel
}

func (a OrganizationEntityAdapter) ToEntity() entities.Organization {
	return entities.Organization{
		ID:          uuid.UUID(a.Model.ID),
		Name:        a.Model.Name,
		Description: a.Model.Description,
		CreatedAt:   a.Model.CreatedAt,
		UpdatedAt:   a.Model.UpdatedAt,
	}
}
