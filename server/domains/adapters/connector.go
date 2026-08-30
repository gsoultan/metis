package adapters

import (
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

type ConnectorModelAdapter struct {
	Connector entities.Connector
}

func (a ConnectorModelAdapter) ToModel() models.Connector {
	schema := make([]models.ConnectorProperty, len(a.Connector.Schema))
	for i, p := range a.Connector.Schema {
		schema[i] = models.ConnectorProperty{
			Key:          p.Key,
			Label:        p.Label,
			Type:         p.Type,
			Description:  p.Description,
			DefaultValue: p.DefaultValue,
			Required:     p.Required,
			Options:      p.Options,
		}
	}
	return models.Connector{
		Base: models.Base{
			ID:        models.UUID(a.Connector.ID),
			CreatedAt: a.Connector.CreatedAt,
		},
		Key:         a.Connector.Key,
		Name:        a.Connector.Name,
		Description: a.Connector.Description,
		Icon:        a.Connector.Icon,
		Type:        a.Connector.Type,
		Schema:      schema,
	}
}

type ConnectorEntityAdapter struct {
	Model models.Connector
}

func (a ConnectorEntityAdapter) ToEntity() entities.Connector {
	schema := make([]entities.ConnectorProperty, len(a.Model.Schema))
	for i, p := range a.Model.Schema {
		schema[i] = entities.ConnectorProperty{
			Key:          p.Key,
			Label:        p.Label,
			Type:         p.Type,
			Description:  p.Description,
			DefaultValue: p.DefaultValue,
			Required:     p.Required,
			Options:      p.Options,
		}
	}
	return entities.Connector{
		ID:          uuid.UUID(a.Model.ID),
		Key:         a.Model.Key,
		Name:        a.Model.Name,
		Description: a.Model.Description,
		Icon:        a.Model.Icon,
		Type:        a.Model.Type,
		Schema:      schema,
		CreatedAt:   a.Model.CreatedAt,
	}
}
