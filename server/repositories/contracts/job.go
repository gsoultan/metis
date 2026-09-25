package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

// JobBacklog is the queue's state at one moment, for the metrics endpoint.
type JobBacklog struct {
	// Due is how many jobs are waiting whose time has come.
	Due int64
	// OldestDue is when the longest-waiting of them came due; zero when none is.
	OldestDue time.Time
	// LeaseExpired is how many a worker claimed and stopped renewing — a worker
	// that died, or one stuck past its lease. The next poll reclaims them.
	LeaseExpired int64
}

type JobRepository interface {
	Create(ctx context.Context, job models.JobModel) (uuid.UUID, error)
	Get(ctx context.Context, id uuid.UUID) (models.JobModel, error)
	Update(ctx context.Context, job models.JobModel) error
	GetPending(ctx context.Context, limit int) ([]models.JobModel, error)
	ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.JobModel, error)
	Lock(ctx context.Context, id uuid.UUID, lockDuration time.Duration, workerID string) (bool, error)

	// Backlog reports the queue across every tenant, so it is refused unless
	// ctx is system work.
	Backlog(ctx context.Context, now time.Time) (JobBacklog, error)
}
