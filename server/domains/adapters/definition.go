package adapters

import (
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

type DefinitionModelAdapter struct {
	Definition *entities.ProcessDefinition
}

func (a DefinitionModelAdapter) ToModel() models.ProcessDefinitionModel {
	var projectID, deploymentID uuid.UUID
	if a.Definition.Project != nil {
		projectID = a.Definition.Project.ID
	}
	if a.Definition.Deployment != nil {
		deploymentID = a.Definition.Deployment.ID
	}
	nodes := make([]models.FlowNode, len(a.Definition.Nodes))
	for i, n := range a.Definition.Nodes {
		if n == nil {
			continue
		}
		nodes[i] = a.nodeToModel(n)
	}
	flows := make([]models.SequenceFlow, len(a.Definition.Flows))
	for i, f := range a.Definition.Flows {
		if f == nil {
			continue
		}
		flows[i] = models.SequenceFlow{
			ID:            f.ID,
			SourceRef:     f.SourceRef,
			TargetRef:     f.TargetRef,
			Condition:     f.Condition,
			Documentation: f.Documentation,
		}
	}
	return models.ProcessDefinitionModel{
		Base: models.Base{
			ID:        a.Definition.ID,
			CreatedAt: a.Definition.CreatedAt,
		},
		ProjectID:    projectID,
		Key:          a.Definition.Key,
		Name:         a.Definition.Name,
		Version:      a.Definition.Version,
		Nodes:        nodes,
		Flows:        flows,
		DeploymentID: deploymentID,
	}
}

func (a DefinitionModelAdapter) nodeToModel(n *entities.Node) models.FlowNode {
	// map child nodes and flows first
	nodes := make([]models.FlowNode, len(n.Nodes))
	for i, cn := range n.Nodes {
		if cn == nil {
			continue
		}
		nodes[i] = a.nodeToModel(cn)
	}
	flows := make([]models.SequenceFlow, len(n.Flows))
	for i, cf := range n.Flows {
		if cf == nil {
			continue
		}
		flows[i] = models.SequenceFlow{
			ID:            cf.ID,
			SourceRef:     cf.SourceRef,
			TargetRef:     cf.TargetRef,
			Condition:     cf.Condition,
			Documentation: cf.Documentation,
		}
	}

	// map candidates (entities -> model strings)
	candidateUsers := make([]string, len(n.CandidateUsers))
	for i, u := range n.CandidateUsers {
		if u != nil {
			candidateUsers[i] = u.Username
		}
	}
	candidateGroups := make([]string, len(n.CandidateGroups))
	for i, g := range n.CandidateGroups {
		if g != nil {
			candidateGroups[i] = g.Name
		}
	}
	return models.FlowNode{
		ID:                  n.ID,
		Name:                n.Name,
		Type:                models.NodeType(n.Type),
		Assignee:            n.Assignee,
		CandidateUsers:      candidateUsers,
		CandidateGroups:     candidateGroups,
		Priority:            n.Priority,
		DueDate:             n.DueDate,
		FormKey:             n.FormKey,
		DefaultFlow:         n.DefaultFlow,
		Script:              n.Script,
		ScriptFormat:        n.ScriptFormat,
		ExternalTopic:       n.ExternalTopic,
		Documentation:       n.Documentation,
		AttachedToRef:       n.AttachedToRef,
		ParentID:            n.ParentID,
		CancelActivity:      n.CancelActivity,
		MultiInstanceType:   n.MultiInstanceType,
		LoopCardinality:     n.LoopCardinality,
		Collection:          n.Collection,
		ElementVariable:     n.ElementVariable,
		CompletionCondition: n.CompletionCondition,
		IsEventSubProcess:   n.IsEventSubProcess,
		Incoming:            n.Incoming,
		Outgoing:            n.Outgoing,
		X:                   n.X,
		Y:                   n.Y,
		Condition:           n.Condition,
		Properties:          n.Properties,
		Nodes:               nodes,
		Flows:               flows,
	}
}

type DefinitionEntityAdapter struct {
	Model models.ProcessDefinitionModel
}

func (a DefinitionEntityAdapter) ToEntity() *entities.ProcessDefinition {
	nodes := make([]*entities.Node, len(a.Model.Nodes))
	for i, n := range a.Model.Nodes {
		nodes[i] = a.nodeToEntity(n)
	}
	flows := make([]*entities.SequenceFlow, len(a.Model.Flows))
	for i, f := range a.Model.Flows {
		cf := &entities.SequenceFlow{
			ID:            f.ID,
			SourceRef:     f.SourceRef,
			TargetRef:     f.TargetRef,
			Condition:     f.Condition,
			Documentation: f.Documentation,
		}
		flows[i] = cf
	}
	return &entities.ProcessDefinition{
		ID:         a.Model.ID,
		Project:    &entities.Project{ID: a.Model.ProjectID},
		Key:        a.Model.Key,
		Name:       a.Model.Name,
		Version:    a.Model.Version,
		Nodes:      nodes,
		Flows:      flows,
		Deployment: &entities.Deployment{ID: a.Model.DeploymentID},
		CreatedAt:  a.Model.CreatedAt,
	}
}

func (a DefinitionEntityAdapter) nodeToEntity(n models.FlowNode) *entities.Node {
	nodes := make([]*entities.Node, len(n.Nodes))
	for i, cn := range n.Nodes {
		nodes[i] = a.nodeToEntity(cn)
	}
	flows := make([]*entities.SequenceFlow, len(n.Flows))
	for i, cf := range n.Flows {
		flows[i] = &entities.SequenceFlow{
			ID:            cf.ID,
			SourceRef:     cf.SourceRef,
			TargetRef:     cf.TargetRef,
			Condition:     cf.Condition,
			Documentation: cf.Documentation,
		}
	}
	// map candidates (model strings -> entities)
	candidateUsers := make([]*entities.User, len(n.CandidateUsers))
	for i, u := range n.CandidateUsers {
		candidateUsers[i] = &entities.User{Username: u}
	}
	candidateGroups := make([]*entities.Group, len(n.CandidateGroups))
	for i, g := range n.CandidateGroups {
		candidateGroups[i] = &entities.Group{Name: g}
	}
	return &entities.Node{
		ID:                  n.ID,
		Name:                n.Name,
		Type:                entities.NodeType(n.Type),
		Assignee:            n.Assignee,
		CandidateUsers:      candidateUsers,
		CandidateGroups:     candidateGroups,
		Priority:            n.Priority,
		DueDate:             n.DueDate,
		FormKey:             n.FormKey,
		DefaultFlow:         n.DefaultFlow,
		Script:              n.Script,
		ScriptFormat:        n.ScriptFormat,
		ExternalTopic:       n.ExternalTopic,
		Documentation:       n.Documentation,
		AttachedToRef:       n.AttachedToRef,
		ParentID:            n.ParentID,
		CancelActivity:      n.CancelActivity,
		MultiInstanceType:   n.MultiInstanceType,
		LoopCardinality:     n.LoopCardinality,
		Collection:          n.Collection,
		ElementVariable:     n.ElementVariable,
		CompletionCondition: n.CompletionCondition,
		IsEventSubProcess:   n.IsEventSubProcess,
		Incoming:            n.Incoming,
		Outgoing:            n.Outgoing,
		X:                   n.X,
		Y:                   n.Y,
		Condition:           n.Condition,
		Properties:          n.Properties,
		Nodes:               nodes,
		Flows:               flows,
	}
}
