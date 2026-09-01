package adapters

import (
	pbentities "github.com/gsoultan/metis/api/proto/entities"
	"github.com/gsoultan/metis/server/domains/entities"
)

type OrganizationPBAdapter struct {
	Organization entities.Organization
}

func (a OrganizationPBAdapter) ToProto() *pbentities.Organization {
	return &pbentities.Organization{
		Id:          a.Organization.ID.String(),
		Name:        a.Organization.Name,
		Description: a.Organization.Description,
	}
}
