package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

type FormRepository interface {
	Create(ctx context.Context, f models.FormModel) error
	Get(ctx context.Context, id uuid.UUID) (models.FormModel, error)
	GetByKey(ctx context.Context, projectID uuid.UUID, key string) (models.FormModel, error)
	ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.FormModel, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
