package adapters

import (
	pbentities "github.com/gsoultan/gobpm/api/proto/entities"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/types/known/structpb"
)

type ExternalTaskPBAdapter struct {
	Task entities.ExternalTask
}

func (a ExternalTaskPBAdapter) ToProto() *pbentities.ExternalTask {
	variables, err := structpb.NewStruct(a.Task.Variables)
	if err != nil {
		// Dropping these silently gave a gRPC caller an object with no
		// variables at all, while the same object over HTTP had them.
		log.Warn().Err(err).Msg("Variables could not be represented in protobuf and were omitted")
	}
	projectID := ""
	if a.Task.Project != nil {
		projectID = a.Task.Project.ID.String()
	}
	instanceID := ""
	if a.Task.ProcessInstance != nil {
		instanceID = a.Task.ProcessInstance.ID.String()
	}
	return &pbentities.ExternalTask{
		Id:        a.Task.ID.String(),
		Project:   projectRef(projectID),
		Instance:  instanceRef(instanceID),
		Node:      NodeToProto(a.Task.Node),
		Topic:     a.Task.Topic,
		Variables: variables,
		Retries:   int32(a.Task.Retries),
	}
}
