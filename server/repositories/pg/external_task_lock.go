package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/store/externaltask"
)

// ExtendLock gives the worker holding a task's lock until lockDuration from
// now, and reports whether it held it.
//
// A lock that has run out is not extended, even while the row still names
// its worker and nobody else has fetched the task. From the moment it ran out
// the task was on offer, and whether anybody took it since is chance — how
// busy the other workers were. Refusing makes the expiry a deadline a worker
// meets every time or misses every time, rather than one it gets away with
// missing until the day another worker is quicker.
//
// One statement, and so its own transaction. Only a refusal reads the task
// again, to tell a task the caller may not see (not found) from a lock that
// is not the worker's any more.
func (r *externalTaskRepository) ExtendLock(ctx context.Context, id uuid.UUID, workerID string, lockDuration time.Duration) (time.Time, bool, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return time.Time{}, false, err
	}
	if !scope.unrestricted() && len(scope.projects) == 0 {
		return time.Time{}, false, fmt.Errorf("%w: no such external task", apierr.ErrNotFound)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return time.Time{}, false, err
	}

	now := time.Now().UTC()
	// The column keeps microseconds: the worker is told the time stored.
	until := now.Add(lockDuration).Truncate(time.Microsecond)
	// Every condition is on the row being written, which is the point. A fetch
	// that locked the task for another worker leaves a row this does not
	// match, whether the fetch committed first or this waited for it:
	// PostgreSQL checks the conditions again against the row it waited for.
	// Reading the row, deciding in Go and writing it back would let a late
	// extension overwrite that fetch.
	var extend externaltask.Mut
	extend.SetLockExpiration(until)
	extend.SetUpdatedAt(now)
	extended, err := externaltask.New().
		Where(externaltask.ID.Eq(id), externaltask.WorkerID.Eq(workerID), externaltask.LockExpiration.Gt(now)).
		WhereIf(!scope.unrestricted(), externaltask.ProjectID.In(uuidsToRaw(scope.projects)...)).
		UpdateAll(ctx, ex, extend)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("could not extend the external task's lock: %w", err)
	}
	if extended > 0 {
		return until, true, nil
	}
	if _, err := r.one(ctx, id); err != nil {
		return time.Time{}, false, err
	}
	return time.Time{}, false, nil
}
