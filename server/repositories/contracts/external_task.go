package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/repositories/models"

	"github.com/google/uuid"
)

// ExternalTaskRepository defines the interface for external task data access.
type ExternalTaskRepository interface {
	Create(ctx context.Context, task *models.ExternalTaskModel) error
	Get(ctx context.Context, id uuid.UUID) (*models.ExternalTaskModel, error)
	Update(ctx context.Context, task *models.ExternalTaskModel) error
	Delete(ctx context.Context, id uuid.UUID) error
	FetchAndLock(ctx context.Context, topic string, workerID string, maxTasks int, lockDuration int64) ([]*models.ExternalTaskModel, error)
	ListByProcessInstance(ctx context.Context, instanceID uuid.UUID) ([]*models.ExternalTaskModel, error)
}
