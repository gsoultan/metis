package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

type IncidentRepository interface {
	Create(ctx context.Context, incident models.IncidentModel) (models.IncidentModel, error)
	Get(ctx context.Context, id uuid.UUID) (models.IncidentModel, error)
	ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.IncidentModel, error)
	Update(ctx context.Context, incident models.IncidentModel) error
	Delete(ctx context.Context, id uuid.UUID) error
}
