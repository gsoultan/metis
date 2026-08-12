package adapters

import (
	pbentities "github.com/gsoultan/gobpm/api/proto/entities"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"google.golang.org/protobuf/types/known/structpb"
)

type ExternalTaskPBAdapter struct {
	Task entities.ExternalTask
}

func (a ExternalTaskPBAdapter) ToProto() *pbentities.ExternalTask {
	variables, _ := structpb.NewStruct(a.Task.Variables)
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
