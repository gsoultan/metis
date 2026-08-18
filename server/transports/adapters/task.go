package adapters

import (
	"github.com/rs/zerolog/log"
	"time"

	pbentities "github.com/gsoultan/gobpm/api/proto/entities"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"google.golang.org/protobuf/types/known/structpb"
)

type TaskPBAdapter struct {
	Task entities.Task
}

func (a TaskPBAdapter) ToProto() *pbentities.Task {
	variables, err := structpb.NewStruct(a.Task.Variables)
	if err != nil {
		// Dropping these silently gave a gRPC caller an object with no
		// variables at all, while the same object over HTTP had them.
		log.Warn().Err(err).Msg("Variables could not be represented in protobuf and were omitted")
	}
	dueDate := ""
	if a.Task.DueDate != nil {
		dueDate = a.Task.DueDate.Format(time.RFC3339)
	}
	projectID := ""
	if a.Task.Project != nil {
		projectID = a.Task.Project.ID.String()
	}
	instanceID := ""
	if a.Task.Instance != nil {
		instanceID = a.Task.Instance.ID.String()
	}
	assignee := ""
	if a.Task.Assignee != nil {
		assignee = a.Task.Assignee.Username
	}
	var candidateUsers []*pbentities.User
	if len(a.Task.CandidateUsers) > 0 {
		candidateUsers = make([]*pbentities.User, 0, len(a.Task.CandidateUsers))
		for _, u := range a.Task.CandidateUsers {
			if u != nil {
				candidateUsers = append(candidateUsers, UserPBAdapter{User: *u}.ToProto())
			}
		}
	}
	var candidateGroups []*pbentities.Group
	if len(a.Task.CandidateGroups) > 0 {
		candidateGroups = make([]*pbentities.Group, 0, len(a.Task.CandidateGroups))
		for _, g := range a.Task.CandidateGroups {
			if g != nil {
				candidateGroups = append(candidateGroups, GroupPBAdapter{Group: *g}.ToProto())
			}
		}
	}
	return &pbentities.Task{
		Id:              a.Task.ID.String(),
		Project:         projectRef(projectID),
		Instance:        instanceRef(instanceID),
		Node:            NodeToProto(a.Task.Node),
		Name:            a.Task.Name,
		Status:          string(a.Task.Status),
		Assignee:        userRef(assignee),
		CandidateUsers:  candidateUsers,
		CandidateGroups: candidateGroups,
		Priority:        int32(a.Task.Priority),
		DueDate:         dueDate,
		CreatedAt:       a.Task.CreatedAt.Format(time.RFC3339),
		Variables:       variables,
		FormKey:         a.Task.FormKey,
		// The form a task is completed with, and the node type that decides
		// whether it has one at all. Both are on the domain task and neither was
		// being sent, so a task with a custom form rendered the default one.
		FormDefinition: a.Task.FormDefinition,
		Type:           string(a.Task.Type),
		Description:    a.Task.Description,
	}
}
