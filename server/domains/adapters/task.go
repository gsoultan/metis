package adapters

import (
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

type TaskModelAdapter struct {
	Task entities.Task
}

func (a TaskModelAdapter) ToModel() models.TaskModel {
	var projectID, instanceID uuid.UUID
	if a.Task.Project != nil {
		projectID = a.Task.Project.ID
	}
	if a.Task.Instance != nil {
		instanceID = a.Task.Instance.ID
	}
	var assignee string
	if a.Task.Assignee != nil {
		assignee = a.Task.Assignee.Username
	}
	var owner string
	if a.Task.Owner != nil {
		owner = a.Task.Owner.Username
	}
	candidateUsers := make([]string, len(a.Task.CandidateUsers))
	for i, u := range a.Task.CandidateUsers {
		if u != nil {
			candidateUsers[i] = u.Username
		}
	}
	candidateGroups := make([]string, len(a.Task.CandidateGroups))
	for i, g := range a.Task.CandidateGroups {
		if g != nil {
			candidateGroups[i] = g.Name
		}
	}
	return models.TaskModel{
		Base: models.Base{
			ID:        models.UUID(a.Task.ID),
			CreatedAt: a.Task.CreatedAt,
		},
		ProjectID:  models.UUID(projectID),
		InstanceID: models.UUID(instanceID),
		NodeID: func() string {
			if a.Task.Node != nil {
				return a.Task.Node.ID
			}
			return ""
		}(),
		IterationID:     a.Task.IterationID,
		Name:            a.Task.Name,
		Description:     a.Task.Description,
		Type:            models.NodeType(a.Task.Type),
		Status:          models.TaskStatus(a.Task.Status),
		Assignee:        assignee,
		Owner:           owner,
		DelegationState: string(a.Task.DelegationState),
		CandidateUsers:  candidateUsers,
		CandidateGroups: candidateGroups,
		Priority:        a.Task.Priority,
		DueDate:         a.Task.DueDate,
		FormKey:         a.Task.FormKey,
		FormDefinition:  a.Task.FormDefinition,
		Variables:       models.EncryptedMap(a.Task.Variables),
	}
}

type TaskEntityAdapter struct {
	Model models.TaskModel
}

func (a TaskEntityAdapter) ToEntity() entities.Task {
	var assignee *entities.User
	if a.Model.Assignee != "" {
		assignee = &entities.User{Username: a.Model.Assignee}
	}
	var owner *entities.User
	if a.Model.Owner != "" {
		owner = &entities.User{Username: a.Model.Owner}
	}
	candidateUsers := make([]*entities.User, len(a.Model.CandidateUsers))
	for i, u := range a.Model.CandidateUsers {
		candidateUsers[i] = &entities.User{Username: u}
	}
	candidateGroups := make([]*entities.Group, len(a.Model.CandidateGroups))
	for i, g := range a.Model.CandidateGroups {
		candidateGroups[i] = &entities.Group{Name: g}
	}

	return entities.Task{
		ID:       uuid.UUID(a.Model.ID),
		Project:  &entities.Project{ID: uuid.UUID(a.Model.ProjectID)},
		Instance: &entities.ProcessInstance{ID: uuid.UUID(a.Model.InstanceID)},
		// The node's Type is carried on the task row, so a response can say what
		// kind of step this is without a join. Name is deliberately not filled
		// in from Model.Name: UpdateTask lets a task be renamed, after which the
		// task's name is no longer the diagram's label for the node, and a
		// caller reading Node.Name would get the newer of the two without any
		// way to tell. Task.Name is the label to display.
		Node:            &entities.Node{ID: a.Model.NodeID, Type: entities.NodeType(a.Model.Type)},
		IterationID:     a.Model.IterationID,
		Name:            a.Model.Name,
		Description:     a.Model.Description,
		Type:            entities.NodeType(a.Model.Type),
		Status:          entities.TaskStatus(a.Model.Status),
		Assignee:        assignee,
		Owner:           owner,
		DelegationState: entities.DelegationState(a.Model.DelegationState),
		CandidateUsers:  candidateUsers,
		CandidateGroups: candidateGroups,
		Priority:        a.Model.Priority,
		DueDate:         a.Model.DueDate,
		FormKey:         a.Model.FormKey,
		FormDefinition:  a.Model.FormDefinition,
		Variables:       a.Model.Variables,
		CreatedAt:       a.Model.CreatedAt,
	}
}
