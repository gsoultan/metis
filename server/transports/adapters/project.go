package adapters

import (
	pbentities "github.com/gsoultan/metis/api/proto/entities"
	"github.com/gsoultan/metis/server/domains/entities"
)

type ProjectPBAdapter struct {
	Project entities.Project
}

func (a ProjectPBAdapter) ToProto() *pbentities.Project {
	orgID := ""
	if a.Project.Organization != nil {
		orgID = a.Project.Organization.ID.String()
	}
	return &pbentities.Project{
		Id:           a.Project.ID.String(),
		Organization: organizationRef(orgID),
		Name:         a.Project.Name,
		Description:  a.Project.Description,
	}
}
