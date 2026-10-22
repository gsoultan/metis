package pg

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/job"
	"github.com/gsoultan/storm/runtime"
)

type jobRepository struct{ conn }

// NewJobRepository returns the queue of work the engine has to come back to —
// timers, retries, service calls.
//
// Deliberately not tenant-scoped. The job worker spans every organization by
// design: one that could only see a single tenant's timers would leave everyone
// else's processes stopped, and there is no request context on a poll loop to
// resolve a tenant from anyway.
func NewJobRepository(c *db.Conn) contracts.JobRepository {
	return &jobRepository{conn{conn: c}}
}

func (r *jobRepository) Create(ctx context.Context, j models.JobModel) (uuid.UUID, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	payload, err := sealedJSONOf(j.Payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("could not encode the job's payload: %w", err)
	}
	ins := job.Create()
	if id := uuid.UUID(j.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetInstanceID(uuid.UUID(j.InstanceID))
	ins.SetDefinitionID(uuid.UUID(j.DefinitionID))
	organization, err := r.organizationOf(ctx, ex, j)
	if err != nil {
		return uuid.Nil, err
	}
	if organization != uuid.Nil {
		ins.SetOrganizationID(organization)
	} else {
		ins.SetOrganizationIDNull()
	}
	ins.SetNodeID(j.NodeID)
	ins.SetType(string(j.Type))
	ins.SetStatus(string(j.Status))
	ins.SetPayload(payload)
	ins.SetRetries(int64(j.Retries))
	ins.SetMaxRetries(int64(j.MaxRetries))
	ins.SetRepeatsRemaining(int64(j.RepeatsRemaining))
	ins.SetNextRunAt(j.NextRunAt)
	setOrNullString(ins.SetIterationID, ins.SetIterationIDNull, j.IterationID)
	setOrNullString(ins.SetLockedBy, ins.SetLockedByNull, j.LockedBy)
	setOrNullString(ins.SetLastError, ins.SetLastErrorNull, j.LastError)
	if j.LockExpires != nil {
		ins.SetLockExpires(*j.LockExpires)
	}
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		return uuid.Nil, fmt.Errorf("could not create the job: %w", err)
	}
	return row.ID, nil
}

func (r *jobRepository) Get(ctx context.Context, id uuid.UUID) (models.JobModel, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.JobModel{}, err
	}
	row, found, err := job.New().Where(job.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return models.JobModel{}, fmt.Errorf("could not read the job: %w", err)
	}
	if !found {
		return models.JobModel{}, fmt.Errorf("%w: no such job", apierr.ErrNotFound)
	}
	return jobFrom(row)
}

func (r *jobRepository) Update(ctx context.Context, j models.JobModel) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	row, found, err := job.New().Where(job.ID.Eq(uuid.UUID(j.ID))).One(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the job: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such job", apierr.ErrNotFound)
	}
	payload, err := sealedJSONOf(j.Payload)
	if err != nil {
		return fmt.Errorf("could not encode the job's payload: %w", err)
	}
	mut := job.Mutate(row)
	// The version as well as the node: a migration moves a job to another
	// version of its process, and a job left naming the old one fires against
	// a definition the instance no longer runs — a moved timer went looking
	// for its new node in the old version and dismissed itself as stale.
	mut.SetDefinitionID(uuid.UUID(j.DefinitionID))
	mut.SetStatus(string(j.Status))
	mut.SetPayload(payload)
	mut.SetRetries(int64(j.Retries))
	mut.SetMaxRetries(int64(j.MaxRetries))
	mut.SetRepeatsRemaining(int64(j.RepeatsRemaining))
	mut.SetNextRunAt(j.NextRunAt)
	mut.SetNodeID(j.NodeID)
	setOrNullString(mut.SetLockedBy, mut.SetLockedByNull, j.LockedBy)
	setOrNullString(mut.SetLastError, mut.SetLastErrorNull, j.LastError)
	if j.LockExpires != nil {
		mut.SetLockExpires(*j.LockExpires)
	} else {
		mut.SetLockExpiresNull()
	}
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not update the job: %w", err)
	}
	return nil
}

// GetPending returns work that is due and not already held, each
// organization's oldest first, in turns.
//
// An expired lock counts as unheld: the worker that took it is gone, and
// leaving its work permanently claimed would strand every process waiting on
// it. A job left running — a killed pod, a crash, a status write that failed —
// is offered again once its lease has expired, and Lock counts reclaiming one
// as an attempt.
//
// In turns because oldest first alone was not fair. One organization that
// queued ten thousand timers at once had every worker until they had all run,
// and another's approval reminder, due a second later, waited behind every one
// of them. Now each organization with work due has its oldest job in the first
// turn, its second oldest in the second, and so on, oldest first within a
// turn: a round of fifty jobs among five organizations runs ten of each if
// they all have ten due, and all of one organization's if nobody else has any.
// Jobs made before migration 44 carry no organization and take their turns as
// one more.
//
// A round looks at no more than claimOwnersPerRound organizations, starting
// from a different place each time; see fairClaimable. Whatever that leaves of
// the round is filled oldest first, as before, so a round is never smaller
// for being fair.
func (r *jobRepository) GetPending(ctx context.Context, limit int) ([]models.JobModel, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	from := uuid.New()
	var offered []turnOf
	for _, status := range []models.JobStatus{models.JobPending, models.JobRunning} {
		fair, err := claimable(ctx, ex, status, now, limit, from)
		if err != nil {
			return nil, fmt.Errorf("could not read the %s jobs that are due: %w", status, err)
		}
		offered = append(offered, fair...)
	}
	slices.SortStableFunc(offered, func(a, b turnOf) int {
		if a.turn != b.turn {
			return cmp.Compare(a.turn, b.turn)
		}
		return a.row.NextRunAt.Compare(b.row.NextRunAt)
	})
	offered = offered[:min(len(offered), limit)]

	rows := make([]job.Row, 0, limit)
	taken := map[[16]byte]bool{}
	for _, o := range offered {
		rows = append(rows, o.row)
		taken[o.row.ID] = true
	}
	if len(rows) < limit {
		rest, err := oldestDue(ctx, ex, now, limit+len(taken))
		if err != nil {
			return nil, err
		}
		for _, row := range rest {
			if len(rows) == limit {
				break
			}
			if !taken[row.ID] {
				rows = append(rows, row)
			}
		}
	}
	return jobsFrom(rows)
}

// oldestDue is the claim as it was before it took turns: the due jobs whose
// lease is free, oldest first, whoever they belong to.
//
// Two ranges rather than one query over both statuses. Each reads the claim
// index in order and stops at the limit; one query over two statuses has to
// gather every due row and sort it — measured with 100,000 jobs due, 18.8ms a
// call against 0.04ms for each range.
func oldestDue(ctx context.Context, ex runtime.Executor, now time.Time, limit int) ([]job.Row, error) {
	var rows []job.Row
	for _, status := range []models.JobStatus{models.JobPending, models.JobRunning} {
		due, err := job.New().
			Where(job.Status.Eq(string(status)), job.NextRunAt.Lte(now)).
			Any(job.LockExpires.Lt(now), job.LockExpires.IsNull()).
			Order(job.NextRunAt.Asc()).
			Limit(int64(limit)).
			All(ctx, ex, nil)
		if err != nil {
			return nil, fmt.Errorf("could not read the %s jobs that are due: %w", status, err)
		}
		rows = append(rows, due...)
	}
	slices.SortFunc(rows, func(a, b job.Row) int { return a.NextRunAt.Compare(b.NextRunAt) })
	return rows, nil
}

// Backlog reports how far behind the queue is, for the metrics endpoint.
//
// Three small reads rather than one aggregate: each is a range on an indexed
// column, and a scrape every few seconds should not be the heaviest query the
// database runs.
// ForgetFinishedBefore deletes, a batch at a time, the jobs that will never
// run again: completed ones, and failed ones no unresolved incident names.
//
// Never a pending or a running job — those are work the engine owes. Never a
// failed job with an incident still open either: resolving the incident is
// what runs it again, and it reads the job to do so, so deleting the row
// would turn "retry" into an error. A failed job with no incident is one whose
// instance had ended; one whose incidents are all resolved was run again and
// failed once more after its instance had ended. Neither will run again.
//
// Old means neither due nor changed since cutoff. next_run_at is in the claim
// index beside status, so each batch is a range of it; updated_at is checked
// as well so a job finished after it was due is kept for the whole period
// from when it finished.
//
// Raw SQL, as every sweep is: storm deletes by primary key, and naming every
// row first would be a read of exactly the rows about to go.
func (r *jobRepository) ForgetFinishedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if !entities.IsSystemContext(ctx) {
		return 0, fmt.Errorf("%w: forgetting finished jobs spans every tenant", apierr.ErrForbidden)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	cutoff = cutoff.UTC()
	completed, err := db.DeleteInBatches(ctx, ex,
		`DELETE FROM jobs WHERE ctid = ANY(ARRAY(
		     SELECT ctid FROM jobs
		      WHERE status = $1 AND next_run_at < $2 AND updated_at < $2
		      LIMIT $3))`, string(models.JobCompleted), cutoff)
	if err != nil {
		return completed, fmt.Errorf("could not forget completed jobs: %w", err)
	}
	failed, err := db.DeleteInBatches(ctx, ex,
		`DELETE FROM jobs WHERE ctid = ANY(ARRAY(
		     SELECT j.ctid FROM jobs j
		      WHERE j.status = $1 AND j.next_run_at < $2 AND j.updated_at < $2
		        AND NOT EXISTS (
		            SELECT 1 FROM incidents i WHERE i.job_id = j.id AND i.status <> $3)
		      LIMIT $4))`, string(models.JobFailed), cutoff, string(models.IncidentResolved))
	if err != nil {
		return completed + failed, fmt.Errorf("could not forget failed jobs: %w", err)
	}
	return completed + failed, nil
}

func (r *jobRepository) Backlog(ctx context.Context, now time.Time) (contracts.JobBacklog, error) {
	if !entities.IsSystemContext(ctx) {
		return contracts.JobBacklog{}, fmt.Errorf("%w: the job backlog spans every tenant", apierr.ErrForbidden)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return contracts.JobBacklog{}, err
	}
	now = now.UTC()
	due := job.New().Where(job.Status.Eq(string(models.JobPending)), job.NextRunAt.Lte(now))

	var backlog contracts.JobBacklog
	if backlog.Due, err = due.Count(ctx, ex); err != nil {
		return contracts.JobBacklog{}, fmt.Errorf("could not count the due jobs: %w", err)
	}
	if backlog.Due > 0 {
		oldest, err := due.Order(job.NextRunAt.Asc()).Limit(1).All(ctx, ex, nil)
		if err != nil {
			return contracts.JobBacklog{}, fmt.Errorf("could not read the oldest due job: %w", err)
		}
		if len(oldest) == 1 {
			backlog.OldestDue = oldest[0].NextRunAt
		}
	}
	if backlog.LeaseExpired, err = job.New().
		Where(job.Status.Eq(string(models.JobRunning)), job.LockExpires.Lt(now)).
		Count(ctx, ex); err != nil {
		return contracts.JobBacklog{}, fmt.Errorf("could not count the jobs whose lease ran out: %w", err)
	}
	return backlog, nil
}

// turnOf is a claimable job and its place in its organization's queue, from 1.
type turnOf struct {
	row  job.Row
	turn int64
}

// claimOwnersPerRound bounds how many organizations one round of the claim
// looks at.
//
// Finding the organizations is a seek each along ix_jobs_claim_by_organization,
// and so is reading each one's due jobs. Measured on PostgreSQL 16 with a
// prepared statement: 4ms a round with 50 organizations holding jobs, 8ms with
// 1,000, 0.8s with 100,000 — and a busy worker runs up to twenty rounds a
// tick. Bounded, a round costs what a thousand do at most — 10ms measured with
// 100,000 organizations holding jobs; an installation with more takes them a
// window at a time, starting at a random place each round, and the rest of
// each round is filled oldest first.
const claimOwnersPerRound = 1000

// fairClaimable lists the due jobs in one status whose lease is free, at most
// $3 of each organization's, numbered in each by age, and the first $3 of
// them by turn and then age.
//
// The organizations are those with any job in the status, found by skipping
// along ix_jobs_claim_by_organization a seek at a time, not read from the
// organizations table: a job whose organization has no row there is still
// owed. At most $5 of them, from the first at or after $4, wrapping round to
// the start. Each one's jobs are then one range of the same index. Jobs with no
// organization, made before migration 44, are one more queue.
//
// Raw SQL because storm has no lateral join or window. The soft-delete
// predicate the store adds to every read is written out.
const fairClaimable = `WITH RECURSIVE onward(id, n) AS (
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
SELECT id, turn FROM queued ORDER BY turn, next_run_at, id LIMIT $3`

// claimable reads the due jobs in one status whose lease is free, never taken
// or expired, in the turns fairClaimable gives them, looking at the
// organizations from the first at or after from.
func claimable(ctx context.Context, ex runtime.Executor, status models.JobStatus, now time.Time, limit int, from uuid.UUID) ([]turnOf, error) {
	rows, err := ex.Query(ctx, fairClaimable, []any{string(status), now, int64(limit), from, int64(claimOwnersPerRound)})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids [][16]byte
	turns := map[[16]byte]int64{}
	for rows.Next() {
		values := rows.RawValues()
		if len(values) < 2 || len(values[0]) != len(uuid.UUID{}) {
			return nil, errors.New("a claimable job's id is not 16 bytes")
		}
		var turn int64
		if err := scanInt(values[1:2], &turn); err != nil {
			return nil, err
		}
		id := [16]byte(values[0])
		ids = append(ids, id)
		turns[id] = turn
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	found, err := job.New().Where(job.ID.In(ids...)).All(ctx, ex, nil)
	if err != nil {
		return nil, err
	}
	offered := make([]turnOf, 0, len(found))
	for _, row := range found {
		offered = append(offered, turnOf{row: row, turn: turns[row.ID]})
	}
	return offered, nil
}

// organizationOf is whose work a job is: the one the caller named, or the
// organization of its instance's project. uuid.Nil when the instance or its
// project is not there to say, which the claim treats as one more queue.
func (r *jobRepository) organizationOf(ctx context.Context, ex runtime.Executor, j models.JobModel) (uuid.UUID, error) {
	if j.OrganizationID != nil && uuid.UUID(*j.OrganizationID) != uuid.Nil {
		return uuid.UUID(*j.OrganizationID), nil
	}
	if uuid.UUID(j.InstanceID) == uuid.Nil {
		return uuid.Nil, nil
	}
	rows, err := ex.Query(ctx, instanceOrganization, []any{uuid.UUID(j.InstanceID)})
	if err != nil {
		return uuid.Nil, fmt.Errorf("could not read whose work the job is: %w", err)
	}
	defer rows.Close()
	organization := uuid.Nil
	if rows.Next() {
		if values := rows.RawValues(); len(values) > 0 && len(values[0]) == len(uuid.UUID{}) {
			organization = uuid.UUID(values[0])
		}
	}
	if err := rows.Err(); err != nil {
		return uuid.Nil, fmt.Errorf("could not read whose work the job is: %w", err)
	}
	return organization, nil
}

// instanceOrganization is an instance's organization, through its project.
// Both are in every database: an environment's has its own project's row.
const instanceOrganization = `SELECT p.organization_id FROM process_instances AS i
JOIN projects AS p ON p.id = i.project_id WHERE i.id = $1`

// ListByInstance returns every job an instance has, soonest first.
//
// Every one, not the store's first thousand. A repeating timer leaves a job
// per occurrence, so a long-running instance collects them, and the one still
// waiting is due last: behind a thousand that already ran, a migration never
// saw it and left it naming the old version.
func (r *jobRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.JobModel, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	// The id breaks ties in due time, so the cursor is a position.
	rows, err := everyRow[job.Row](ctx, ex, job.New().
		Where(job.InstanceID.Eq(instanceID)).
		Order(job.NextRunAt.Asc(), job.ID.Asc()))
	if err != nil {
		return nil, fmt.Errorf("could not read the instance's jobs: %w", err)
	}
	return jobsFrom(rows)
}

// Lock claims a job for one worker, reporting whether the claim succeeded.
//
// A conditional update, and the condition is the whole mechanism: two workers
// racing for the same job both run this, the database applies one of them, and
// the loser sees no rows changed. Reading first and updating second would let
// both read "pending" and both proceed.
//
// Raw SQL because the predicate is part of the write. Reading the row, deciding
// in Go and writing back is exactly the pattern this avoids.
//
// Reclaiming a job whose worker died is counted as an attempt, in the same
// statement: the attempt it died in never reached the code that counts
// failures. Otherwise a job that kills its worker every time — a script that
// runs a pod out of memory — would be picked up by the next pod, and the next,
// forever, instead of ending as an incident somebody can look at. The SET reads
// the row as it was, so only a claim of a running job counts.
func (r *jobRepository) Lock(ctx context.Context, id uuid.UUID, lockDuration time.Duration, workerID string) (bool, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	claimed, err := ex.Exec(ctx,
		`UPDATE jobs SET status = $1, locked_by = $2, lock_expires = $3, updated_at = now(),
		        retries = retries + CASE WHEN status = $1 THEN 1 ELSE 0 END
		  WHERE id = $4 AND (status = $5 OR (status = $1 AND lock_expires < $6))`,
		[]any{
			string(models.JobRunning), workerID, now.Add(lockDuration),
			id, string(models.JobPending), now,
		})
	if err != nil {
		return false, fmt.Errorf("could not lock the job: %w", err)
	}
	return claimed > 0, nil
}

func jobsFrom(rows []job.Row) ([]models.JobModel, error) {
	out := make([]models.JobModel, 0, len(rows))
	for _, row := range rows {
		j, err := jobFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, nil
}

func jobFrom(row job.Row) (models.JobModel, error) {
	payload, err := sealedMapOf(row.Payload)
	if err != nil {
		return models.JobModel{}, fmt.Errorf("could not decode a job's payload: %w", err)
	}
	j := models.JobModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		InstanceID:       models.UUID(row.InstanceID),
		DefinitionID:     models.UUID(row.DefinitionID),
		NodeID:           row.NodeID,
		IterationID:      valueOr(row.IterationID),
		Type:             models.JobType(row.Type),
		Status:           models.JobStatus(row.Status),
		LockedBy:         valueOr(row.LockedBy),
		Payload:          payload,
		Retries:          int(row.Retries),
		MaxRetries:       int(row.MaxRetries),
		RepeatsRemaining: int(row.RepeatsRemaining),
		NextRunAt:        row.NextRunAt,
		LastError:        valueOr(row.LastError),
	}
	if expires, ok := row.LockExpires.Get(); ok {
		j.LockExpires = &expires
	}
	if organization, ok := row.OrganizationID.Get(); ok {
		id := models.UUID(organization)
		j.OrganizationID = &id
	}
	return j, nil
}
