package model

import (
	"github.com/gsoultan/storm"
)

// Queries is every statement written in SQL rather than built from the model,
// for the generator to PREPARE against the model and give a typed scanner.
//
// A statement declared here and not listed is refused when it runs: storm only
// runs the text it was shown at generate time, which is what stops a string
// put together at run time from passing for one of these. A statement listed
// here whose columns drift from the model fails `make generate`, naming the
// column, instead of failing a request.
//
// Each lives here rather than beside its repository because the generator
// reads this package and nothing else; the repository says why it is SQL.
func Queries() []storm.RawDecl {
	return []storm.RawDecl{
		FairClaimableJobs,
		ForgetFinishedInstances,
	}
}

// ClaimTurn is one due job and its turn in the claim: its place among its
// organization's due jobs, oldest first.
type ClaimTurn struct {
	ID   [16]byte
	Turn int64
}

// FairClaimableJobs lists the due jobs in one status whose lease is free, at
// most $3 of each organization's, numbered in each by age, and the first $3 of
// them by turn and then age.
//
// The organizations are those with any job in the status, found by skipping
// along ix_jobs_claim_by_organization a seek at a time, not read from the
// organizations table: a job whose organization has no row there is still
// owed. At most $5 of them, from the first at or after $4, wrapping round to
// the start. Each one's jobs are then one range of the same index. Jobs with no
// organization, made before migration 44, are one more queue.
//
// SQL because storm has no recursive CTE of the caller's own, lateral join or
// window. The soft-delete predicate the store adds to every read is written
// out.
var FairClaimableJobs = storm.SQL[ClaimTurn](`WITH RECURSIVE onward(id, n) AS (
  (SELECT organization_id, 1 FROM jobs
    WHERE status = $1 AND organization_id >= $4
    ORDER BY organization_id LIMIT 1)
  UNION ALL
  SELECT (SELECT j.organization_id FROM jobs AS j
           WHERE j.status = $1 AND j.organization_id > onward.id
           ORDER BY j.organization_id LIMIT 1), onward.n + 1
    FROM onward WHERE onward.id IS NOT NULL AND onward.n < $5
), wrapped(id, n) AS (
  (SELECT organization_id, 1 FROM jobs
    WHERE status = $1 AND organization_id < $4
    ORDER BY organization_id LIMIT 1)
  UNION ALL
  SELECT (SELECT j.organization_id FROM jobs AS j
           WHERE j.status = $1 AND j.organization_id > wrapped.id AND j.organization_id < $4
           ORDER BY j.organization_id LIMIT 1), wrapped.n + 1
    FROM wrapped WHERE wrapped.id IS NOT NULL AND wrapped.n < $5
), owners AS (
  SELECT id FROM (
    SELECT id FROM onward WHERE id IS NOT NULL
    UNION ALL
    SELECT id FROM wrapped WHERE id IS NOT NULL
  ) AS found LIMIT $5
), queued AS (
  SELECT q.id, q.next_run_at, q.turn FROM owners
  CROSS JOIN LATERAL (
    SELECT id, next_run_at, row_number() OVER (ORDER BY next_run_at, id) AS turn
      FROM jobs
     WHERE status = $1 AND organization_id = owners.id AND next_run_at <= $2
       AND (lock_expires IS NULL OR lock_expires < $2) AND deleted_at IS NULL
     ORDER BY next_run_at, id
     LIMIT $3) AS q
  UNION ALL
  SELECT id, next_run_at, row_number() OVER (ORDER BY next_run_at, id) FROM (
    SELECT id, next_run_at FROM jobs
     WHERE status = $1 AND organization_id IS NULL AND next_run_at <= $2
       AND (lock_expires IS NULL OR lock_expires < $2) AND deleted_at IS NULL
     ORDER BY next_run_at, id
     LIMIT $3) AS unowned
)
SELECT id, turn FROM queued ORDER BY turn, next_run_at, id LIMIT $3`)

// ForgetFinishedInstances deletes one batch of finished instances and the
// rows that exist only to run them, in one statement so a batch goes whole or
// not at all. $1 the finished states, $2 the earliest any period ends, $3
// resolved, $4 the batch, $5 now, $6 the installation's period in days or
// null.
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
//
// SQL because storm has no recursive CTE of the caller's own and no
// data-modifying CTE.
var ForgetFinishedInstances = storm.SQLExec(`WITH RECURSIVE held_up AS (
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
DELETE FROM process_instances WHERE id IN (SELECT id FROM doomed)`)
