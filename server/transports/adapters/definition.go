package adapters

import (
	pbentities "github.com/gsoultan/gobpm/api/proto/entities"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

type ProcessDefinitionPBAdapter struct {
	Definition *entities.ProcessDefinition
}

func (a ProcessDefinitionPBAdapter) ToProto() *pbentities.ProcessDefinition {
	projectID := ""
	if a.Definition.Project != nil {
		projectID = a.Definition.Project.ID.String()
	}
	return &pbentities.ProcessDefinition{
		Id:        a.Definition.ID.String(),
		ProjectId: projectID,
		Key:       a.Definition.Key,
		Name:      a.Definition.Name,
		Version:   int32(a.Definition.Version),
	}
}
