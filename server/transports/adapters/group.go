package adapters

import (
	pbentities "github.com/gsoultan/gobpm/api/proto/entities"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

type GroupPBAdapter struct {
	Group entities.Group
}

func (a GroupPBAdapter) ToProto() *pbentities.Group {
	orgID := ""
	if a.Group.Organization != nil {
		orgID = a.Group.Organization.ID.String()
	}
	return &pbentities.Group{
		Id:           a.Group.ID.String(),
		Organization: organizationRef(orgID),
		Name:         a.Group.Name,
		Description:  a.Group.Description,
	}
}
