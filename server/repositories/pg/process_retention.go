package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
)

// lockRetentionHolds waits for every transaction that is placing or
// releasing a hold, or changing a process's period, and keeps new ones
// waiting until the batch commits.
//
// The batch reads the holds and the periods once, when its statement starts.
// A hold placed after that and committed before the batch reached its
// instance would not be seen, and the instance it was placed to keep would
// go; so would the instances of a process whose period had just been made
// longer. SHARE conflicts with the lock every insert and delete takes, and
// not with itself, so sweeps on several replicas still run side by side. A
// hold or a period is an administrator's act, and waits for one batch at
// most.
const lockRetentionHolds = `LOCK TABLE retention_holds, retention_periods IN SHARE MODE`

// ForgetFinished deletes, a batch at a time, the instances that finished
// longer ago than their period and that nothing still needs, with their
// tasks, jobs, external tasks, service calls, incidents, subscriptions,
// compensation records, variable snapshots and notifications. It returns how
// many instances went.
//
// fallback is the installation's period, for the processes that have none of
// their own; zero means the installation has none, and then only the
// instances of processes given a period are removed.
//
// Finished means completed or cancelled. A failed instance is not touched:
// somebody may still act on it.
//
// Each batch is a transaction of its own, so the lock on the holds is let go
// between batches and a sweep that is clearing a backlog does not keep a hold
// waiting for all of it.
func (r *processRepository) ForgetFinished(ctx context.Context, now time.Time, fallback time.Duration) (int64, error) {
	if !entities.IsSystemContext(ctx) {
		return 0, fmt.Errorf("%w: forgetting finished instances spans every tenant", apierr.ErrForbidden)
	}
	if db.InTransaction(ctx) {
		return 0, errors.New("forgetting finished instances commits a batch at a time, and must not be started inside a transaction")
	}
	shortest, err := r.shortestPeriod(ctx)
	if err != nil {
		return 0, err
	}
	var fallbackDays any // null: the installation keeps instances for ever
	if days := int64(fallback / (24 * time.Hour)); days > 0 {
		fallbackDays = days
		if shortest == 0 || days < shortest {
			shortest = days
		}
	}
	if shortest == 0 {
		return 0, nil // no period anywhere: nothing can qualify
	}
	now = now.UTC()
	finished := []string{string(models.ProcessCompleted), string(models.ProcessCancelled)}
	earliest := now.Add(-time.Duration(shortest) * 24 * time.Hour)
	args := []any{finished, earliest, string(models.IncidentResolved), db.PruneBatch, now, fallbackDays}
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		removed, err := r.forgetOneBatch(ctx, args)
		total += removed
		if err != nil {
			return total, fmt.Errorf("could not forget finished instances: %w", err)
		}
		if removed < db.PruneBatch {
			return total, nil
		}
	}
}

// shortestPeriod reads model.ShortestRetentionPeriod; zero when no process
// has one.
func (r *processRepository) shortestPeriod(ctx context.Context) (int64, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	period, _, err := model.ShortestRetentionPeriod.One(ctx, ex)
	if err != nil {
		return 0, fmt.Errorf("could not read the processes' retention periods: %w", err)
	}
	return int64(period.Days.V), nil
}

// forgetOneBatch removes one batch, with the holds and the periods held
// still for as long as it takes.
func (r *processRepository) forgetOneBatch(ctx context.Context, args []any) (int64, error) {
	var removed int64
	err := r.conn.conn.Transact(ctx, func(ctx context.Context) error {
		ex, err := r.conn.conn.Executor(ctx)
		if err != nil {
			return err
		}
		if _, err := ex.Exec(ctx, lockRetentionHolds, nil); err != nil {
			return fmt.Errorf("wait for the holds being placed: %w", err)
		}
		removed, err = model.ForgetFinishedInstances.Exec(ctx, ex, args...)
		return err
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

func (r *processRepository) Existing(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	if !entities.IsSystemContext(ctx) {
		return nil, fmt.Errorf("%w: asking which instances exist spans every tenant", apierr.ErrForbidden)
	}
	found := make(map[uuid.UUID]bool, len(ids))
	if len(ids) == 0 {
		return found, nil
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	// WithDeleted: a removed instance still has a row, and it is not one to
	// recreate.
	rows, err := processinstance.New().WithDeleted().
		Where(processinstance.ID.In(uuidsToRaw(ids)...)).
		Limit(int64(len(ids))).
		AllRef(ctx, ex)
	if err != nil {
		return nil, fmt.Errorf("could not read which instances exist: %w", err)
	}
	for _, row := range rows {
		found[row.ID] = true
	}
	return found, nil
}
