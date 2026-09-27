package adapters

import (
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
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

		RolesByOrganization: rolesByOrganizationModel(a.User.RolesByOrganization),
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
	// The issuer alone: it says how the account signs in. The subject is the
	// provider's identifier for the person, and nothing but the sign-in that
	// looks an account up by it has a use for it.
	var identityProvider string
	if a.Model.IdentityIssuer != nil {
		identityProvider = *a.Model.IdentityIssuer
	}
	return entities.User{
		ID:               uuid.UUID(a.Model.ID),
		Organizations:    orgs,
		Projects:         projects,
		Username:         a.Model.Username,
		FullName:         a.Model.FullName,
		DisplayName:      a.Model.DisplayName,
		Organization:     org,
		Email:            a.Model.Email,
		Roles:            a.Model.Roles,
		CreatedAt:        a.Model.CreatedAt,
		IdentityProvider: identityProvider,

		RolesByOrganization: rolesByOrganizationEntity(a.Model.RolesByOrganization),
	}
}

// rolesByOrganizationModel and rolesByOrganizationEntity carry an account's
// roles in each organization across the two id types. Nil for none, so an
// account that holds no role in any one organization costs no map.
func rolesByOrganizationModel(held map[uuid.UUID][]string) map[models.UUID][]string {
	if len(held) == 0 {
		return nil
	}
	out := make(map[models.UUID][]string, len(held))
	for organization, roles := range held {
		out[models.UUID(organization)] = roles
	}
	return out
}

func rolesByOrganizationEntity(held map[models.UUID][]string) map[uuid.UUID][]string {
	if len(held) == 0 {
		return nil
	}
	out := make(map[uuid.UUID][]string, len(held))
	for organization, roles := range held {
		out[uuid.UUID(organization)] = roles
	}
	return out
}
