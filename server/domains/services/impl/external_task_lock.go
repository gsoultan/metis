package impl

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// ExtendLock gives the worker holding a task's lock more time: the lock runs
// out lockDuration milliseconds from now, not from when it would have.
//
// A lock is a lease so that a worker that dies does not strand its work, and
// fetch-and-lock was the only way to take one. Work that outlasted it was
// offered to the next worker to ask while the first was still doing it, and
// the step ran twice. A worker can now keep its lease short and renew it for
// as long as it is working.
//
// Only the worker holding the lock, and only before it runs out. A refusal is
// a 400, as a claim on a task somebody else has claimed is: asking again will
// not help, and the worker should stop and fetch.
func (s *externalTaskService) ExtendLock(ctx context.Context, taskID uuid.UUID, workerID string, lockDuration int64) (time.Time, error) {
	if err := validateLockExtension(workerID, lockDuration); err != nil {
		return time.Time{}, err
	}
	until, held, err := s.repo.ExternalTask().ExtendLock(ctx, taskID, workerID, time.Duration(lockDuration)*time.Millisecond)
	if err != nil {
		return time.Time{}, err
	}
	if !held {
		return time.Time{}, apierr.Invalidf("worker %q does not hold the lock on this task: it has run out, or it is another "+
			"worker's, and the task may already be with another worker — fetch it again", workerID)
	}
	return until, nil
}

// validateLockExtension refuses an extension that cannot mean anything: no
// worker named, or no time, or more than the longest a lock may be.
//
// No worker is refused rather than matched: a lock taken with an empty
// worker id is one every nameless worker would hold.
func validateLockExtension(workerID string, lockDuration int64) error {
	if strings.TrimSpace(workerID) == "" {
		return apierr.Invalidf("worker_id is empty; name the worker that holds the lock")
	}
	longest := entities.MaxExternalTaskLock.Milliseconds()
	if lockDuration <= 0 || lockDuration > longest {
		return apierr.Invalidf("lock_duration_ms is %d; it must be from 1 to %d, a day", lockDuration, longest)
	}
	return nil
}
