package adapters

import (
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

type GroupModelAdapter struct {
	Group entities.Group
}

func (a GroupModelAdapter) ToModel() models.GroupModel {
	var orgID uuid.UUID
	if a.Group.Organization != nil {
		orgID = a.Group.Organization.ID
	}
	return models.GroupModel{
		Base: models.Base{
			ID:        models.UUID(a.Group.ID),
			CreatedAt: a.Group.CreatedAt,
		},
		OrganizationID: models.UUID(orgID),
		Name:           a.Group.Name,
		Description:    a.Group.Description,
		Roles:          a.Group.Roles,
	}
}

type GroupEntityAdapter struct {
	Model models.GroupModel
}

func (a GroupEntityAdapter) ToEntity() entities.Group {
	var org *entities.Organization
	if a.Model.OrganizationID != models.NilUUID {
		org = &entities.Organization{ID: uuid.UUID(a.Model.OrganizationID)}
	}
	return entities.Group{
		ID:           uuid.UUID(a.Model.ID),
		Organization: org,
		Name:         a.Model.Name,
		Description:  a.Model.Description,
		Roles:        a.Model.Roles,
		CreatedAt:    a.Model.CreatedAt,
	}
}
