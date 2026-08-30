package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"

	"github.com/google/uuid"
)

// ExternalTaskService defines the business logic for external tasks.
type ExternalTaskService interface {
	FetchAndLock(ctx context.Context, topic string, workerID string, maxTasks int, lockDuration int64) ([]*entities.ExternalTask, error)
	Complete(ctx context.Context, taskID uuid.UUID, workerID string, variables map[string]any) error
	HandleFailure(ctx context.Context, taskID uuid.UUID, workerID string, errorMessage string, errorDetails string, retries int, retryTimeout int64) error
	Create(ctx context.Context, task *entities.ExternalTask) error
}
