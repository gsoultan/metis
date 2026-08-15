package adapters

import (
	pbentities "github.com/gsoultan/gobpm/api/proto/entities"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

type UserPBAdapter struct {
	User entities.User
}

func (a UserPBAdapter) ToProto() *pbentities.User {
	orgs := make([]*pbentities.UserOrganization, 0, len(a.User.Organizations))
	for _, o := range a.User.Organizations {
		if o != nil {
			orgs = append(orgs, &pbentities.UserOrganization{
				Id:   o.ID.String(),
				Name: o.Name,
			})
		}
	}
	projects := make([]*pbentities.UserProject, 0, len(a.User.Projects))
	for _, p := range a.User.Projects {
		if p != nil {
			projects = append(projects, &pbentities.UserProject{
				Id:   p.ID.String(),
				Name: p.Name,
			})
		}
	}
	org := ""
	if a.User.Organization != nil {
		org = a.User.Organization.Name
	}
	return &pbentities.User{
		Id:            a.User.ID.String(),
		Organizations: orgs,
		Projects:      projects,
		Username:      a.User.Username,
		FullName:      a.User.FullName,
		DisplayName:   a.User.DisplayName,
		Organization:  organizationRef(org),
		Email:         a.User.Email,
		Roles:         a.User.Roles,
	}
}
