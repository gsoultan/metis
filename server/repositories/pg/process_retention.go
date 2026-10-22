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
)

// forgetFinishedInstances deletes one batch of finished instances and the
// rows that exist only to run them, in one statement so a batch goes whole or
// not at all.
//
// An instance qualifies when it completed or was cancelled longer ago than
// its period, and nothing still needs it: no deviation was recorded or asked
// for on it (those are compliance records, kept with what they are about), no
// incident on it is open, its parent has finished too, and so have its
// children, and no retention hold keeps it.
//
// Its period is the one an administrator last gave its process (by key, for
// every version), and the installation's where the process has none, or was
// returned to the installation's. An instance whose process has no period
// while the installation has none either is kept: $6 is then null, and so is
// the cutoff it would be compared with. $2 is the earliest any period ends,
// so the scan starts from the instances that could qualify at all.
//
// A hold keeps the whole case the instance it names belongs to: the instance
// that started the chain of calls it is part of, and every instance started
// from that one, at any depth. Somebody reading a held case finds the
// instance it was called from and the ones it called. The walk up to the
// first instance and back down starts from the holds, which are few, and not
// from the instances.
//
// The audit trail is not touched: it is the record of who did what, and is
// kept for as long as the installation is.
const forgetFinishedInstances = `WITH RECURSIVE held_up AS (
	SELECT h.instance_id AS id FROM retention_holds h
	UNION
	SELECT p.parent_instance_id FROM process_instances p JOIN held_up u ON p.id = u.id
	 WHERE p.parent_instance_id IS NOT NULL
), held AS (
	SELECT id FROM held_up
	UNION
	SELECT c.id FROM process_instances c JOIN held t ON c.parent_instance_id = t.id
), doomed AS (
	SELECT pi.id FROM process_instances pi
	 WHERE pi.status = ANY($1) AND pi.updated_at < $2
	   AND pi.updated_at < $5::timestamptz - make_interval(days => COALESCE((
	         SELECT rp.days FROM retention_periods rp
	           JOIN process_definitions d ON d.id = pi.definition_id
	          WHERE rp.project_id = pi.project_id AND rp.process_key = d.key
	          ORDER BY rp.created_at DESC, rp.id DESC
	          LIMIT 1), $6::int))
	   AND NOT EXISTS (SELECT 1 FROM held WHERE held.id = pi.id)
	   AND NOT EXISTS (SELECT 1 FROM instance_deviations d WHERE d.instance_id = pi.id)
	   AND NOT EXISTS (SELECT 1 FROM deviation_requests r WHERE r.instance_id = pi.id)
	   AND NOT EXISTS (SELECT 1 FROM incidents i WHERE i.instance_id = pi.id AND i.status <> $3)
	   AND NOT EXISTS (SELECT 1 FROM process_instances c
	                    WHERE c.parent_instance_id = pi.id AND NOT (c.status = ANY($1)))
	   AND NOT EXISTS (SELECT 1 FROM process_instances p
	                    WHERE p.id = pi.parent_instance_id AND NOT (p.status = ANY($1)))
	 LIMIT $4
	 FOR UPDATE SKIP LOCKED
), tasks_gone AS (
	DELETE FROM tasks WHERE instance_id IN (SELECT id FROM doomed)
), jobs_gone AS (
	DELETE FROM jobs WHERE instance_id IN (SELECT id FROM doomed)
), external_tasks_gone AS (
	DELETE FROM external_tasks WHERE instance_id IN (SELECT id FROM doomed)
), service_calls_gone AS (
	DELETE FROM service_calls WHERE instance_id IN (SELECT id FROM doomed)
), incidents_gone AS (
	DELETE FROM incidents WHERE instance_id IN (SELECT id FROM doomed)
), subscriptions_gone AS (
	DELETE FROM event_subscriptions WHERE instance_id IN (SELECT id FROM doomed)
), compensations_gone AS (
	DELETE FROM compensatable_activities WHERE instance_id IN (SELECT id FROM doomed)
), snapshots_gone AS (
	DELETE FROM variable_snapshots WHERE instance_id IN (SELECT id FROM doomed)
), notifications_gone AS (
	DELETE FROM notifications WHERE instance_id IN (SELECT id FROM doomed)
)
DELETE FROM process_instances WHERE id IN (SELECT id FROM doomed)`

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

// shortestRetentionPeriod is the shortest period any process has now, in
// days, or null when none has one.
const shortestRetentionPeriod = `SELECT min(days) FROM (
	SELECT DISTINCT ON (project_id, process_key) days FROM retention_periods
	 ORDER BY project_id, process_key, created_at DESC, id DESC
) newest`

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

// shortestPeriod reads shortestRetentionPeriod; zero when no process has one.
func (r *processRepository) shortestPeriod(ctx context.Context) (int64, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	rows, err := ex.Query(ctx, shortestRetentionPeriod, nil)
	if err != nil {
		return 0, fmt.Errorf("could not read the processes' retention periods: %w", err)
	}
	defer rows.Close()
	var shortest int64
	if rows.Next() {
		if err := scanInt(rows.RawValues(), &shortest); err != nil {
			return 0, err
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("could not read the processes' retention periods: %w", err)
	}
	return shortest, nil
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
		removed, err = ex.Exec(ctx, forgetFinishedInstances, args)
		return err
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

// existingInstancesSQL answers which of a list of ids have a row. Read by the
// primary key, a batch at a time.
const existingInstancesSQL = `SELECT id FROM process_instances WHERE id = ANY($1::uuid[])`

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
	texts := make([]string, len(ids))
	for i, id := range ids {
		texts[i] = id.String()
	}
	rows, err := ex.Query(ctx, existingInstancesSQL, []any{texts})
	if err != nil {
		return nil, fmt.Errorf("could not read which instances exist: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		values := rows.RawValues()
		var id uuid.UUID
		if len(values) == 0 || len(values[0]) != len(id) {
			return nil, errors.New("could not read which instances exist: an id is not a uuid")
		}
		copy(id[:], values[0])
		found[id] = true
	}
	return found, rows.Err()
}
