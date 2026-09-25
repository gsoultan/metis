package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/externaltask"
	"github.com/gsoultan/storm/runtime"
)

type externalTaskRepository struct{ conn }

// NewExternalTaskRepository returns the work parked for outside workers.
func NewExternalTaskRepository(c *db.Conn) contracts.ExternalTaskRepository {
	return &externalTaskRepository{conn{conn: c}}
}

// Create parks work for a worker to claim.
//
// Written by the engine as it reaches the node, so it is not scoped: a refusal
// would leave an instance waiting on a task that was never offered.
func (r *externalTaskRepository) Create(ctx context.Context, task *models.ExternalTaskModel) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	variables, err := sealedJSONOf(task.Variables)
	if err != nil {
		return fmt.Errorf("could not encode the task's variables: %w", err)
	}
	ins := externaltask.Create()
	if id := uuid.UUID(task.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetProjectID(uuid.UUID(task.ProjectID))
	ins.SetInstanceID(uuid.UUID(task.ProcessInstanceID))
	ins.SetDefinitionID(uuid.UUID(task.ProcessDefinitionID))
	ins.SetNodeID(task.NodeID)
	ins.SetTopic(task.Topic)
	ins.SetRetries(int64(task.Retries))
	ins.SetRetryTimeout(task.RetryTimeout)
	ins.SetVariables(variables)
	applyLock(ins.SetWorkerID, ins.SetWorkerIDNull, ins.SetLockExpiration, ins.SetLockExpirationNull, task)
	setOrNullString(ins.SetErrorMessage, ins.SetErrorMessageNull, task.ErrorMessage)
	setOrNullString(ins.SetErrorDetails, ins.SetErrorDetailsNull, task.ErrorDetails)
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not park the external task: %w", err)
	}
	created, err := externalTaskFrom(row)
	if err != nil {
		return err
	}
	*task = *created
	return nil
}

func (r *externalTaskRepository) Get(ctx context.Context, id uuid.UUID) (*models.ExternalTaskModel, error) {
	row, err := r.one(ctx, id)
	if err != nil {
		return nil, err
	}
	return externalTaskFrom(row)
}

func (r *externalTaskRepository) Update(ctx context.Context, task *models.ExternalTaskModel) error {
	row, err := r.one(ctx, uuid.UUID(task.ID))
	if err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	variables, err := sealedJSONOf(task.Variables)
	if err != nil {
		return fmt.Errorf("could not encode the task's variables: %w", err)
	}
	mut := externaltask.Mutate(row)
	mut.SetTopic(task.Topic)
	mut.SetRetries(int64(task.Retries))
	mut.SetRetryTimeout(task.RetryTimeout)
	mut.SetVariables(variables)
	applyLock(mut.SetWorkerID, mut.SetWorkerIDNull, mut.SetLockExpiration, mut.SetLockExpirationNull, task)
	setOrNullString(mut.SetErrorMessage, mut.SetErrorMessageNull, task.ErrorMessage)
	setOrNullString(mut.SetErrorDetails, mut.SetErrorDetailsNull, task.ErrorDetails)
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not update the external task: %w", err)
	}
	return nil
}

func (r *externalTaskRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := r.one(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	if err := externaltask.Delete(ctx, ex, id); err != nil {
		if errors.Is(err, runtime.ErrNoRow) {
			return fmt.Errorf("%w: no such external task", apierr.ErrNotFound)
		}
		return fmt.Errorf("could not delete the external task: %w", err)
	}
	return nil
}

// FetchAndLock hands a worker some work and marks it theirs.
//
// One transaction with FOR UPDATE SKIP LOCKED, which is the whole design: two
// workers polling the same topic at the same moment must not be handed the same
// task. SKIP LOCKED lets the second worker step over rows the first is claiming
// rather than queue behind them, so throughput is the number of workers rather
// than the speed of the slowest claim.
//
// The lock is a lease, not a mutex: it expires, so a worker that dies without
// completing its task has the work offered to somebody else rather than
// stranding it.
func (r *externalTaskRepository) FetchAndLock(ctx context.Context, topic, workerID string, maxTasks int, lockDuration int64) ([]*models.ExternalTaskModel, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	if !scope.unrestricted() && len(scope.projects) == 0 {
		return nil, nil
	}

	var claimed []*models.ExternalTaskModel
	err = r.conn.conn.Transact(ctx, func(txCtx context.Context) error {
		ex, err := r.conn.conn.Executor(txCtx)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		// The expired test comes before the null test, and must: the
		// generated builder drops the predicate after a null test inside Any,
		// so the old order matched unlocked tasks only, and a task whose
		// worker died holding it was never offered to anybody again.
		q := externaltask.New().
			Where(externaltask.Topic.Eq(topic)).
			Any(externaltask.LockExpiration.Lt(now), externaltask.LockExpiration.IsNull()).
			Where(externaltask.Retries.Gte(0)).
			Limit(int64(maxTasks)).
			ForUpdateSkipLocked()
		if !scope.unrestricted() {
			q = q.Where(externaltask.ProjectID.In(uuidsToRaw(scope.projects)...))
		}
		rows, err := q.All(txCtx, ex, nil)
		if err != nil {
			return fmt.Errorf("could not read the available external tasks: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}

		expiration := now.Add(time.Duration(lockDuration) * time.Millisecond)
		for _, row := range rows {
			mut := externaltask.Mutate(row)
			mut.SetWorkerID(workerID)
			mut.SetLockExpiration(expiration)
			if err := mut.Update(txCtx, ex); err != nil {
				return fmt.Errorf("could not lock an external task: %w", err)
			}
			// The row is returned to the caller as claimed, so it carries the
			// lock the update just wrote rather than the empty one it was read
			// with — a worker handed a task with no worker id on it would have
			// no way to tell it holds the lease.
			row.WorkerID = runtime.Null[string]{V: workerID, Valid: true}
			row.LockExpiration = runtime.Null[time.Time]{V: expiration, Valid: true}
			task, err := externalTaskFrom(row)
			if err != nil {
				return err
			}
			claimed = append(claimed, task)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

// ListByProcessInstance returns one instance's parked work.
//
// Another tenant's instance is answered with nothing rather than a refusal: a
// list's contract is rows, every caller already handles finding none, and an
// error would tell the caller the instance exists.
func (r *externalTaskRepository) ListByProcessInstance(ctx context.Context, instanceID uuid.UUID) ([]*models.ExternalTaskModel, error) {
	if !r.canSeeInstance(ctx, instanceID) {
		return nil, nil
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := externaltask.New().
		Where(externaltask.InstanceID.Eq(instanceID)).
		Order(externaltask.CreatedAt.Asc()).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list the external tasks: %w", err)
	}
	out := make([]*models.ExternalTaskModel, 0, len(rows))
	for _, row := range rows {
		task, err := externalTaskFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, nil
}

func (r *externalTaskRepository) one(ctx context.Context, id uuid.UUID) (externaltask.Row, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return externaltask.Row{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return externaltask.Row{}, err
	}
	q := externaltask.New().Where(externaltask.ID.Eq(id))
	if !scope.unrestricted() {
		if len(scope.projects) == 0 {
			return externaltask.Row{}, fmt.Errorf("%w: no such external task", apierr.ErrNotFound)
		}
		q = q.Where(externaltask.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	row, found, err := q.One(ctx, ex)
	if err != nil {
		return externaltask.Row{}, fmt.Errorf("could not read the external task: %w", err)
	}
	if !found {
		return externaltask.Row{}, fmt.Errorf("%w: no such external task", apierr.ErrNotFound)
	}
	return row, nil
}

// applyLock writes the worker and its lease, or clears both.
//
// Both together, always: a worker id with no expiry is a lock nothing releases,
// and an expiry with no worker is a lease belonging to nobody.
func applyLock(setWorker func(string), clearWorker func(), setExpiry func(time.Time), clearExpiry func(), task *models.ExternalTaskModel) {
	if task.WorkerID != "" {
		setWorker(task.WorkerID)
	} else {
		clearWorker()
	}
	if task.LockExpiration != nil {
		setExpiry(*task.LockExpiration)
	} else {
		clearExpiry()
	}
}

func externalTaskFrom(row externaltask.Row) (*models.ExternalTaskModel, error) {
	variables, err := sealedMapOf(row.Variables)
	if err != nil {
		return nil, fmt.Errorf("could not decode an external task's variables: %w", err)
	}
	task := &models.ExternalTaskModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		ProjectID:           models.UUID(row.ProjectID),
		ProcessInstanceID:   models.UUID(row.InstanceID),
		ProcessDefinitionID: models.UUID(row.DefinitionID),
		NodeID:              row.NodeID,
		Topic:               row.Topic,
		WorkerID:            valueOr(row.WorkerID),
		Retries:             int(row.Retries),
		RetryTimeout:        row.RetryTimeout,
		ErrorMessage:        valueOr(row.ErrorMessage),
		ErrorDetails:        valueOr(row.ErrorDetails),
		Variables:           variables,
	}
	if expiration, ok := row.LockExpiration.Get(); ok {
		task.LockExpiration = &expiration
	}
	return task, nil
}
