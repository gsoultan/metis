package adapters

import (
	pbentities "github.com/gsoultan/gobpm/api/proto/entities"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/types/known/structpb"
)

type ProcessInstancePBAdapter struct {
	Instance entities.ProcessInstance
}

func (a ProcessInstancePBAdapter) ToProto() *pbentities.ProcessInstance {
	variables, err := structpb.NewStruct(a.Instance.Variables)
	if err != nil {
		// Dropping these silently gave a gRPC caller an object with no
		// variables at all, while the same object over HTTP had them.
		log.Warn().Err(err).Msg("Variables could not be represented in protobuf and were omitted")
	}
	activeNodes := make([]string, len(a.Instance.Tokens))
	for i, t := range a.Instance.Tokens {
		if t.Node != nil {
			activeNodes[i] = t.Node.ID
		}
	}
	projectID := ""
	if a.Instance.Project != nil {
		projectID = a.Instance.Project.ID.String()
	}
	definitionID := ""
	if a.Instance.Definition != nil {
		definitionID = a.Instance.Definition.ID.String()
	}
	return &pbentities.ProcessInstance{
		Id:          a.Instance.ID.String(),
		Project:     projectRef(projectID),
		Definition:  definitionRef(definitionID),
		Status:      string(a.Instance.Status),
		Variables:   variables,
		ActiveNodes: nodeRefs(activeNodes),
	}
}
