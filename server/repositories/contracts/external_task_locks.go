package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

// ExternalTaskLocks is how a worker comes to hold external tasks, and keeps
// holding one. A lock is a lease that runs out, so that a worker that dies
// does not strand its work; the price is that work outlasting the lock is
// offered to somebody else unless the worker holding it extends it.
type ExternalTaskLocks interface {
	FetchAndLock(ctx context.Context, topic string, workerID string, maxTasks int, lockDuration int64) ([]*models.ExternalTaskModel, error)

	// ExtendLock moves the lock workerID holds on a task to lockDuration from
	// now, and returns when it now runs out. held is false, and nothing is
	// written, when the lock has run out or is another worker's; a task the
	// caller may not see is apierr.ErrNotFound.
	ExtendLock(ctx context.Context, id uuid.UUID, workerID string, lockDuration time.Duration) (until time.Time, held bool, err error)
}
