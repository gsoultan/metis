package pg

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
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
	payload, err := jsonOf(j.Payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("could not encode the job's payload: %w", err)
	}
	ins := job.Create()
	if id := uuid.UUID(j.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetInstanceID(uuid.UUID(j.InstanceID))
	ins.SetDefinitionID(uuid.UUID(j.DefinitionID))
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
	payload, err := jsonOf(j.Payload)
	if err != nil {
		return fmt.Errorf("could not encode the job's payload: %w", err)
	}
	mut := job.Mutate(row)
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

// GetPending returns work that is due and not already held, soonest first.
//
// The predicate and the order are the index the model declares, so this is the
// query the queue was shaped for. An expired lock counts as unheld: the worker
// that took it is gone, and leaving its work permanently claimed would strand
// every process waiting on it.
//
// That was this comment's promise and not the query's: it asked for pending
// jobs only, so a job left running — a killed pod, a crash, a status write that
// failed — was never offered to anybody again, and the process waiting behind
// it hung with no incident. Running jobs whose lease has expired are offered
// now, and Lock counts reclaiming one as an attempt.
//
// Two ranges rather than one query over both statuses. Each reads the claim
// index in order and stops at the limit; one query over two statuses has to
// gather every due row and sort it — measured with 100,000 jobs due, 18.8ms a
// call against 0.04ms for each range, on a loop that can run twenty rounds a
// tick exactly when the engine is busiest.
func (r *jobRepository) GetPending(ctx context.Context, limit int) ([]models.JobModel, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	due, err := claimable(ctx, ex, models.JobPending, now, limit)
	if err != nil {
		return nil, fmt.Errorf("could not read the pending jobs: %w", err)
	}
	abandoned, err := claimable(ctx, ex, models.JobRunning, now, limit)
	if err != nil {
		return nil, fmt.Errorf("could not read the jobs whose worker stopped: %w", err)
	}
	rows := slices.Concat(due, abandoned)
	slices.SortFunc(rows, func(a, b job.Row) int { return a.NextRunAt.Compare(b.NextRunAt) })
	return jobsFrom(rows[:min(len(rows), limit)])
}

// claimable reads the due jobs in one status whose lease is free: never taken,
// or expired.
//
// The expired test comes before the null test, and must: the generated builder
// drops the predicate after a null test inside Any, so the order the queue
// used to ask in matched free leases only and never an expired one.
func claimable(ctx context.Context, ex runtime.Executor, status models.JobStatus, now time.Time, limit int) ([]job.Row, error) {
	return job.New().
		Where(job.Status.Eq(string(status)), job.NextRunAt.Lte(now)).
		Any(job.LockExpires.Lt(now), job.LockExpires.IsNull()).
		Order(job.NextRunAt.Asc()).
		Limit(int64(limit)).
		All(ctx, ex, nil)
}

func (r *jobRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.JobModel, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := job.New().
		Where(job.InstanceID.Eq(instanceID)).
		Order(job.NextRunAt.Asc()).
		All(ctx, ex, nil)
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
	payload, err := mapOf(row.Payload)
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
	return j, nil
}
