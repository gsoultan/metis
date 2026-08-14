package adapters

import (
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

type InstanceModelAdapter struct {
	Instance entities.ProcessInstance
}

func (a InstanceModelAdapter) ToModel() models.ProcessInstanceModel {
	var projectID, definitionID uuid.UUID
	var parentInstanceID *uuid.UUID

	if a.Instance.Project != nil {
		projectID = a.Instance.Project.ID
	}
	if a.Instance.Definition != nil {
		definitionID = a.Instance.Definition.ID
	}
	if a.Instance.ParentInstance != nil {
		parentInstanceID = &a.Instance.ParentInstance.ID
	}

	tokens := make([]models.Token, len(a.Instance.Tokens))
	for i, t := range a.Instance.Tokens {
		var instID uuid.UUID
		if t.Instance != nil {
			instID = t.Instance.ID
		}
		tokens[i] = models.Token{
			ID:         models.UUID(t.ID),
			InstanceID: models.UUID(instID),
			NodeID: func() string {
				if t.Node != nil {
					return t.Node.ID
				}
				return ""
			}(),
			Status:      models.TokenStatus(t.Status),
			IterationID: t.IterationID,
			Variables:   t.Variables,
			CreatedAt:   t.CreatedAt,
		}
	}
	return models.ProcessInstanceModel{
		Base: models.Base{
			ID:        models.UUID(a.Instance.ID),
			CreatedAt: a.Instance.CreatedAt,
		},
		ProjectID:        models.UUID(projectID),
		DefinitionID:     models.UUID(definitionID),
		ParentInstanceID: models.FromUUIDPtr(parentInstanceID),
		ParentNodeID: func() string {
			if a.Instance.ParentNode != nil {
				return a.Instance.ParentNode.ID
			}
			return ""
		}(),
		Status:    models.ProcessStatus(a.Instance.Status),
		Variables: models.EncryptedMap(a.Instance.Variables),
		Tokens:    tokens,
		CompletedNodes: func() []string {
			ids := make([]string, len(a.Instance.CompletedNodes))
			for i, n := range a.Instance.CompletedNodes {
				if n != nil {
					ids[i] = n.ID
				}
			}
			return ids
		}(),
		MultiInstance: func() map[string]models.MultiInstanceStateModel {
			if len(a.Instance.MultiInstance) == 0 {
				return nil
			}
			out := make(map[string]models.MultiInstanceStateModel, len(a.Instance.MultiInstance))
			for nodeID, st := range a.Instance.MultiInstance {
				out[nodeID] = models.MultiInstanceStateModel{Total: st.Total, Completed: st.Completed}
			}
			return out
		}(),
		CompensatedNodes: func() []string {
			ids := make([]string, len(a.Instance.CompensatedNodes))
			for i, n := range a.Instance.CompensatedNodes {
				if n != nil {
					ids[i] = n.ID
				}
			}
			return ids
		}(),
	}
}

type InstanceEntityAdapter struct {
	Model models.ProcessInstanceModel
}

func (a InstanceEntityAdapter) ToEntity() entities.ProcessInstance {
	var parentInstance *entities.ProcessInstance
	if a.Model.ParentInstanceID != nil {
		parentInstance = &entities.ProcessInstance{ID: uuid.UUID(*a.Model.ParentInstanceID)}
	}

	tokens := make([]entities.Token, len(a.Model.Tokens))
	for i, t := range a.Model.Tokens {
		tokens[i] = entities.Token{
			ID:          uuid.UUID(t.ID),
			Instance:    &entities.ProcessInstance{ID: uuid.UUID(t.InstanceID)},
			Node:        &entities.Node{ID: t.NodeID},
			Status:      entities.TokenStatus(t.Status),
			IterationID: t.IterationID,
			Variables:   t.Variables,
			CreatedAt:   t.CreatedAt,
		}
	}
	return entities.ProcessInstance{
		ID:             uuid.UUID(a.Model.ID),
		Project:        &entities.Project{ID: uuid.UUID(a.Model.ProjectID)},
		Definition:     &entities.ProcessDefinition{ID: uuid.UUID(a.Model.DefinitionID)},
		ParentInstance: parentInstance,
		ParentNode:     &entities.Node{ID: a.Model.ParentNodeID},
		Status:         entities.ProcessStatus(a.Model.Status),
		Variables:      a.Model.Variables,
		Tokens:         tokens,
		CompletedNodes: func() []*entities.Node {
			nodes := make([]*entities.Node, len(a.Model.CompletedNodes))
			for i, id := range a.Model.CompletedNodes {
				nodes[i] = &entities.Node{ID: id}
			}
			return nodes
		}(),
		MultiInstance: func() map[string]entities.MultiInstanceState {
			if len(a.Model.MultiInstance) == 0 {
				return nil
			}
			out := make(map[string]entities.MultiInstanceState, len(a.Model.MultiInstance))
			for nodeID, st := range a.Model.MultiInstance {
				out[nodeID] = entities.MultiInstanceState{Total: st.Total, Completed: st.Completed}
			}
			return out
		}(),
		CompensatedNodes: func() []*entities.Node {
			nodes := make([]*entities.Node, len(a.Model.CompensatedNodes))
			for i, id := range a.Model.CompensatedNodes {
				nodes[i] = &entities.Node{ID: id}
			}
			return nodes
		}(),
		CreatedAt: a.Model.CreatedAt,
	}
}
