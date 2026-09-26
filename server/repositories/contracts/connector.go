package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

// ConnectorRepository handles the storage of connector templates.
type ConnectorRepository interface {
	List(ctx context.Context) ([]models.Connector, error)
	Get(ctx context.Context, id uuid.UUID) (models.Connector, error)
	GetByKey(ctx context.Context, key string) (models.Connector, error)
	Create(ctx context.Context, connector models.Connector) (models.Connector, error)
	Update(ctx context.Context, connector models.Connector) error
	Delete(ctx context.Context, id uuid.UUID) error

	// RestoreByKey brings back the entry most recently removed under key,
	// keeping its id, so the steps and connections that name it reach it
	// again. Not found when none was removed; the caller makes sure no live
	// entry holds the key.
	RestoreByKey(ctx context.Context, key string) (models.Connector, error)
}

// ConnectorInstanceRepository handles the storage of project-specific connector configurations.
type ConnectorInstanceRepository interface {
	ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.ConnectorInstance, error)
	Get(ctx context.Context, id uuid.UUID) (models.ConnectorInstance, error)
	GetByProjectAndConnector(ctx context.Context, projectID, connectorID uuid.UUID) (models.ConnectorInstance, error)
	Create(ctx context.Context, instance models.ConnectorInstance) (models.ConnectorInstance, error)
	Update(ctx context.Context, instance models.ConnectorInstance) error
	Delete(ctx context.Context, id uuid.UUID) error
}
