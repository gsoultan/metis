package adapters

import (
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

type UserModelAdapter struct {
	User entities.User
}

func (a UserModelAdapter) ToModel() models.UserModel {
	var orgs []models.OrganizationModel
	for _, o := range a.User.Organizations {
		if o != nil {
			orgs = append(orgs, models.OrganizationModel{Base: models.Base{ID: models.UUID(o.ID)}})
		}
	}
	var projects []models.ProjectModel
	for _, p := range a.User.Projects {
		if p != nil {
			projects = append(projects, models.ProjectModel{Base: models.Base{ID: models.UUID(p.ID)}})
		}
	}
	var org string
	if a.User.Organization != nil {
		org = a.User.Organization.Name
	}
	return models.UserModel{
		Base: models.Base{
			ID:        models.UUID(a.User.ID),
			CreatedAt: a.User.CreatedAt,
		},
		Username:      a.User.Username,
		FullName:      a.User.FullName,
		DisplayName:   a.User.DisplayName,
		Organization:  org,
		Email:         a.User.Email,
		Roles:         a.User.Roles,
		Organizations: orgs,
		Projects:      projects,
	}
}

type UserEntityAdapter struct {
	Model models.UserModel
}

func (a UserEntityAdapter) ToEntity() entities.User {
	var orgs []*entities.Organization
	for _, o := range a.Model.Organizations {
		orgs = append(orgs, &entities.Organization{
			ID:          uuid.UUID(o.ID),
			Name:        o.Name,
			Description: o.Description,
			CreatedAt:   o.CreatedAt,
			UpdatedAt:   o.UpdatedAt,
		})
	}
	var projects []*entities.Project
	for _, p := range a.Model.Projects {
		projects = append(projects, &entities.Project{
			ID:           uuid.UUID(p.ID),
			Organization: &entities.Organization{ID: uuid.UUID(p.OrganizationID)},
			Name:         p.Name,
			Description:  p.Description,
			CreatedAt:    p.CreatedAt,
			UpdatedAt:    p.UpdatedAt,
		})
	}
	var org *entities.Organization
	if a.Model.Organization != "" {
		org = &entities.Organization{Name: a.Model.Organization}
	}
	return entities.User{
		ID:            uuid.UUID(a.Model.ID),
		Organizations: orgs,
		Projects:      projects,
		Username:      a.Model.Username,
		FullName:      a.Model.FullName,
		DisplayName:   a.Model.DisplayName,
		Organization:  org,
		Email:         a.Model.Email,
		Roles:         a.Model.Roles,
		CreatedAt:     a.Model.CreatedAt,
	}
}
