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
