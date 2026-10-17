package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/metis/server/repositories/store/retentionhold"
	"github.com/gsoultan/storm/runtime"
)

type retentionHoldRepository struct{ conn }

// NewRetentionHoldRepository returns the store of the holds that keep
// instances out of the finished-instance sweep.
func NewRetentionHoldRepository(c *db.Conn) contracts.RetentionHoldRepository {
	return &retentionHoldRepository{conn{conn: c}}
}

// heldInstanceIndex is the unique index that allows an instance one hold.
const heldInstanceIndex = "ux_retention_holds_instance"

// errHeldInAnEnvironment refuses a hold on an instance that runs in an
// environment's own database: the finished-instance sweep never reaches one,
// so there is nothing to keep it from.
var errHeldInAnEnvironment = apierr.Invalidf("instances in an environment's own database are never removed by the retention sweep, so there is nothing to hold them against")

// Place records a hold, inside the caller's transaction and nowhere else.
//
// The hold is written before the instance is looked for, and that order is
// what keeps a sweep from removing an instance somebody has just held. The
// insert waits for a sweep batch that has the holds to itself
// (ForgetFinishedBefore), and the read after it then sees what that batch
// left: an instance it removed is not found, and the hold goes with the
// transaction it was written in.
func (r *retentionHoldRepository) Place(ctx context.Context, hold entities.RetentionHold) (entities.RetentionHold, error) {
	if !db.InTransaction(ctx) {
		return entities.RetentionHold{}, contracts.ErrRetentionHoldOutsideTransaction
	}
	if _, bound := db.EnvironmentFrom(ctx); bound {
		return entities.RetentionHold{}, errHeldInAnEnvironment
	}
	if hold.Project == nil || hold.Instance == nil {
		return entities.RetentionHold{}, errors.New("a retention hold names a project and an instance")
	}
	if err := r.requireProjectInTenant(ctx, hold.Project.ID); err != nil {
		return entities.RetentionHold{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.RetentionHold{}, err
	}
	ins := stageHold(hold)
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		if heldAlready(err) {
			return entities.RetentionHold{}, contracts.ErrInstanceAlreadyHeld
		}
		return entities.RetentionHold{}, fmt.Errorf("could not record the hold: %w", err)
	}
	if err := r.requireInstanceInProject(ctx, ex, hold.Instance.ID, hold.Project.ID); err != nil {
		return entities.RetentionHold{}, err
	}
	return holdFrom(row), nil
}

// requireInstanceInProject refuses an instance that is not in the project:
// one that was never there, one of another project, and one a sweep has just
// removed are each answered as not found.
func (r *retentionHoldRepository) requireInstanceInProject(ctx context.Context, ex runtime.Executor, instanceID, projectID uuid.UUID) error {
	found, err := processinstance.New().
		Where(processinstance.ID.Eq(instanceID), processinstance.ProjectID.Eq(projectID)).
		Exists(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not check the process instance: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such process instance", apierr.ErrNotFound)
	}
	return nil
}

// Release removes the hold on an instance of the caller's and returns it.
func (r *retentionHoldRepository) Release(ctx context.Context, instanceID uuid.UUID) (entities.RetentionHold, error) {
	if !db.InTransaction(ctx) {
		return entities.RetentionHold{}, contracts.ErrRetentionHoldOutsideTransaction
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.RetentionHold{}, err
	}
	row, found, err := retentionhold.New().Where(retentionhold.InstanceID.Eq(instanceID)).ForUpdate().One(ctx, ex)
	if err != nil {
		return entities.RetentionHold{}, fmt.Errorf("could not read the hold: %w", err)
	}
	if !found || r.requireProjectInTenant(ctx, row.ProjectID) != nil {
		return entities.RetentionHold{}, fmt.Errorf("%w: this instance is not held", apierr.ErrNotFound)
	}
	if err := retentionhold.Delete(ctx, ex, row.ID); err != nil {
		return entities.RetentionHold{}, fmt.Errorf("could not release the hold: %w", err)
	}
	return holdFrom(row), nil
}

// ForInstance reads the hold on an instance. A hold in a project that is not
// the caller's is answered as no hold.
func (r *retentionHoldRepository) ForInstance(ctx context.Context, instanceID uuid.UUID) (entities.RetentionHold, bool, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.RetentionHold{}, false, err
	}
	row, found, err := retentionhold.New().Where(retentionhold.InstanceID.Eq(instanceID)).One(ctx, ex)
	if err != nil {
		return entities.RetentionHold{}, false, fmt.Errorf("could not read the hold: %w", err)
	}
	if !found {
		return entities.RetentionHold{}, false, nil
	}
	if err := r.requireProjectInTenant(ctx, row.ProjectID); err != nil {
		if errors.Is(err, apierr.ErrNotFound) {
			return entities.RetentionHold{}, false, nil
		}
		return entities.RetentionHold{}, false, err
	}
	return holdFrom(row), true, nil
}

// ListByProject lists a project's holds, newest first.
func (r *retentionHoldRepository) ListByProject(ctx context.Context, projectID uuid.UUID, limit int64) ([]entities.RetentionHold, error) {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := retentionhold.New().
		Where(retentionhold.ProjectID.Eq(projectID)).
		Order(retentionhold.CreatedAt.Desc(), retentionhold.ID.Desc()).
		Limit(limit).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list the project's holds: %w", err)
	}
	holds := make([]entities.RetentionHold, 0, len(rows))
	for _, row := range rows {
		holds = append(holds, holdFrom(row))
	}
	return holds, nil
}

// heldAlready reports whether an insert was refused because the instance
// already has a hold. The primary key's refusal is not that.
func heldAlready(err error) bool {
	var refused *runtime.ConstraintError
	return errors.As(err, &refused) && errors.Is(err, runtime.ErrUniqueViolation) && refused.Constraint == heldInstanceIndex
}

// stageHold sets every column of a new hold. Its id is a v7: the list orders
// ties in creation time by id.
func stageHold(hold entities.RetentionHold) retentionhold.Ins {
	id := hold.ID
	if id == uuid.Nil {
		id = uuid.Must(uuid.NewV7())
	}
	ins := retentionhold.Create()
	ins.SetID(id)
	ins.SetProjectID(hold.Project.ID)
	ins.SetInstanceID(hold.Instance.ID)
	ins.SetReason(hold.Reason)
	ins.SetPlacedBy(hold.PlacedBy)
	ins.SetPlacedByID(hold.PlacedByID)
	return ins
}

func holdFrom(row retentionhold.Row) entities.RetentionHold {
	return entities.RetentionHold{
		ID:         row.ID,
		CreatedAt:  row.CreatedAt,
		Project:    &entities.Project{ID: row.ProjectID},
		Instance:   &entities.ProcessInstance{ID: row.InstanceID},
		Reason:     row.Reason,
		PlacedBy:   row.PlacedBy,
		PlacedByID: row.PlacedByID,
	}
}
