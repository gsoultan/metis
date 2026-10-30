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
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/metis/server/repositories/store/retentionperiod"
	"github.com/gsoultan/storm/runtime"
)

// archiveBatch is how many instances one transaction of the sweep archives.
// Far fewer than a prune's batch: each instance is read whole, with
// everything it owns, and its record is built in memory before it is written.
const archiveBatch = 100

// ArchiveFinished moves into the archive, a batch at a time, the instances
// that finished longer ago than their period and that nothing still needs.
// Each is written whole to archived_instances (see archiveInstances), and
// then its tasks, jobs, external tasks, service calls, incidents,
// subscriptions, compensation records, variable snapshots, notifications and
// the audit entries that name nobody are removed from the live tables. It
// returns how many instances went.
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
func (r *processRepository) ArchiveFinished(ctx context.Context, now time.Time, fallback time.Duration) (int64, error) {
	if !entities.IsSystemContext(ctx) {
		return 0, fmt.Errorf("%w: archiving finished instances spans every tenant", apierr.ErrForbidden)
	}
	if db.InTransaction(ctx) {
		return 0, errors.New("archiving finished instances commits a batch at a time, and must not be started inside a transaction")
	}
	shortest, err := r.shortestPeriod(ctx)
	if err != nil {
		return 0, err
	}
	var fallbackDays int64 // zero: the installation keeps instances for ever
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
	batch := forgetBatch{
		finished: []string{string(models.ProcessCompleted), string(models.ProcessCancelled)},
		earliest: now.Add(-time.Duration(shortest) * 24 * time.Hour),
		now:      now,
		fallback: fallbackDays,
		size:     archiveBatch,
	}
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		removed, err := r.archiveOneBatch(ctx, batch)
		total += removed
		if err != nil {
			return total, fmt.Errorf("could not archive finished instances: %w", err)
		}
		if removed < int64(batch.size) {
			return total, nil
		}
	}
}

// shortestPeriod is the shortest period any process has now, in days: the
// least of each process's newest row; zero when no process has one.
func (r *processRepository) shortestPeriod(ctx context.Context) (int64, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	periods, err := newestPeriods(ctx, ex)
	if err != nil {
		return 0, err
	}
	var shortest runtime.Null[int32]
	for _, days := range periods {
		if days.Valid && (!shortest.Valid || days.V < shortest.V) {
			shortest = days
		}
	}
	return int64(shortest.V), nil
}

// processKey names a process, every version of it, inside its project.
type processKey struct {
	project [16]byte
	key     string
}

// newestPeriods is each process's newest retention period row: its days, or
// null where the newest row returned the process to the installation's.
//
// A page at a time, by offset, because a keyset cursor cannot follow an order
// that mixes directions; there is a row per process that has a period, so
// there is rarely a second page.
func newestPeriods(ctx context.Context, ex runtime.Executor) (map[processKey]runtime.Null[int32], error) {
	newest := retentionperiod.New().
		Order(retentionperiod.ProjectID.Asc(), retentionperiod.ProcessKey.Asc(),
			retentionperiod.CreatedAt.Desc(), retentionperiod.ID.Desc()).
		Limit(db.PruneBatch)
	periods := map[processKey]runtime.Null[int32]{}
	var rows []retentionperiod.Row
	for offset := int64(0); ; offset += db.PruneBatch {
		var err error
		rows, err = newest.Offset(offset).AllDistinctOn(ctx, ex, 2, rows[:0])
		if err != nil {
			return nil, fmt.Errorf("could not read the processes' retention periods: %w", err)
		}
		for _, p := range rows {
			periods[processKey{p.ProjectID, p.ProcessKey}] = p.Days
		}
		if len(rows) < db.PruneBatch {
			return periods, nil
		}
	}
}

// archiveOneBatch archives one batch, with the holds and the periods held
// still for as long as it takes.
func (r *processRepository) archiveOneBatch(ctx context.Context, batch forgetBatch) (int64, error) {
	var removed int64
	err := r.conn.conn.Transact(ctx, func(ctx context.Context) error {
		ex, err := r.conn.conn.Executor(ctx)
		if err != nil {
			return err
		}
		if err := lockRetentionHolds(ctx, ex); err != nil {
			return fmt.Errorf("wait for the holds being placed: %w", err)
		}
		doomed, err := batch.doomed(ctx, ex)
		if err != nil || len(doomed) == 0 {
			return err
		}
		if err := archiveInstances(ctx, ex, doomed, batch.now); err != nil {
			return err
		}
		removed, err = forgetInstances(ctx, ex, doomed)
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

// lockRetentionHolds locks the holds and the periods: it waits for every transaction that is placing or
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
func lockRetentionHolds(ctx context.Context, ex runtime.Executor) error {
	return store.LockTables(ctx, ex, store.LockShare, store.TableRetentionHold, store.TableRetentionPeriod)
}
