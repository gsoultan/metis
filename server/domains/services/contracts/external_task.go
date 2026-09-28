package contracts

import (
	"context"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"

	"github.com/google/uuid"
)

// ExternalTaskService defines the business logic for external tasks.
type ExternalTaskService interface {
	FetchAndLock(ctx context.Context, topic string, workerID string, maxTasks int, lockDuration int64) ([]*entities.ExternalTask, error)
	Complete(ctx context.Context, taskID uuid.UUID, workerID string, variables map[string]any) error
	HandleFailure(ctx context.Context, taskID uuid.UUID, workerID string, errorMessage string, errorDetails string, retries int, retryTimeout int64) error
	Create(ctx context.Context, task *entities.ExternalTask) error

	// ExtendLock gives the worker holding a task's lock until lockDuration
	// milliseconds from now, and returns when the lock now runs out. Anybody
	// else, and that worker once its lock has run out, is refused.
	ExtendLock(ctx context.Context, taskID uuid.UUID, workerID string, lockDuration int64) (time.Time, error)
}
