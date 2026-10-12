package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
)

// forgetFinishedInstances deletes one batch of finished instances and the
// rows that exist only to run them, in one statement so a batch goes whole or
// not at all.
//
// An instance qualifies when it completed or was cancelled before the cutoff
// and nothing still needs it: no deviation was recorded or asked for on it
// (those are compliance records, kept with what they are about), no incident
// on it is open, its parent has finished too, and so have its children.
//
// The audit trail is not touched: it is the record of who did what, and is
// kept for as long as the installation is.
const forgetFinishedInstances = `WITH doomed AS (
	SELECT pi.id FROM process_instances pi
	 WHERE pi.status = ANY($1) AND pi.updated_at < $2
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

// ForgetFinishedBefore deletes, a batch at a time, the instances that
// finished before cutoff and that nothing still needs, with their tasks,
// jobs, external tasks, service calls, incidents, subscriptions, compensation
// records, variable snapshots and notifications. It returns how many
// instances went.
//
// Finished means completed or cancelled. A failed instance is not touched:
// somebody may still act on it.
func (r *processRepository) ForgetFinishedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if !entities.IsSystemContext(ctx) {
		return 0, fmt.Errorf("%w: forgetting finished instances spans every tenant", apierr.ErrForbidden)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	finished := []string{string(models.ProcessCompleted), string(models.ProcessCancelled)}
	removed, err := db.DeleteInBatches(ctx, ex, forgetFinishedInstances,
		finished, cutoff.UTC(), string(models.IncidentResolved))
	if err != nil {
		return removed, fmt.Errorf("could not forget finished instances: %w", err)
	}
	return removed, nil
}
