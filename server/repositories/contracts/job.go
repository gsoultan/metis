package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

type JobRepository interface {
	Create(ctx context.Context, job models.JobModel) (uuid.UUID, error)
	Get(ctx context.Context, id uuid.UUID) (models.JobModel, error)
	Update(ctx context.Context, job models.JobModel) error
	GetPending(ctx context.Context, limit int) ([]models.JobModel, error)
	ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.JobModel, error)
	Lock(ctx context.Context, id uuid.UUID, lockDuration time.Duration, workerID string) (bool, error)
}
