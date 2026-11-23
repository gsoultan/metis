package contracts

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

// ErrJobLeaseLost is Settle's refusal: the job is no longer running under the
// worker that tried to record its outcome, because its lease ran out and
// another worker claimed it, or because something else settled it.
var ErrJobLeaseLost = errors.New("the job is no longer held by this worker")

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

	// Renew extends the lease of a running job workerID holds, and reports
	// whether it still held it. An expired lease is not renewed.
	Renew(ctx context.Context, id uuid.UUID, workerID string, lockDuration time.Duration) (bool, error)

	// Settle writes a job's outcome only while workerID still holds it
	// running, and returns ErrJobLeaseLost otherwise.
	Settle(ctx context.Context, job models.JobModel, workerID string) error

	// Backlog reports the queue across every tenant, so it is refused unless
	// ctx is system work.
	Backlog(ctx context.Context, now time.Time) (JobBacklog, error)

	// ForgetFinishedBefore deletes the jobs that will never run again and
	// have been neither due nor changed since cutoff, and returns how many it
	// deleted. Across every tenant, so it is refused unless ctx is system work.
	ForgetFinishedBefore(ctx context.Context, cutoff time.Time) (int64, error)
}
