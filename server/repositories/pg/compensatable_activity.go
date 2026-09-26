package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/compensatableactivity"
	"github.com/gsoultan/storm/runtime"
)

type compensatableActivityRepository struct{ conn }

// NewCompensatableActivityRepository returns the record of work that can still
// be undone.
func NewCompensatableActivityRepository(c *db.Conn) contracts.CompensatableActivityRepository {
	return &compensatableActivityRepository{conn{conn: c}}
}

// Create records an activity that completed and has a compensation handler.
//
// Written on the path of the activity it describes, so it is not scoped: a
// refusal here would leave work that cannot be undone with nothing to say so.
func (r *compensatableActivityRepository) Create(ctx context.Context, m models.CompensatableActivityModel) (models.CompensatableActivityModel, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.CompensatableActivityModel{}, err
	}
	variables, err := sealedJSONOf(m.Variables)
	if err != nil {
		return models.CompensatableActivityModel{}, fmt.Errorf("could not encode the activity's variables: %w", err)
	}

	ins := compensatableactivity.Create()
	if id := uuid.UUID(m.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetInstanceID(uuid.UUID(m.InstanceID))
	ins.SetNodeID(m.NodeID)
	ins.SetCompensationNodeID(m.CompensationNodeID)
	ins.SetVariables(variables)
	ins.SetCompletedAt(m.CompletedAt)
	ins.SetCompensated(m.Compensated)
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		return models.CompensatableActivityModel{}, fmt.Errorf("could not record the compensatable activity: %w", err)
	}
	return compensatableFrom(row)
}

// ListByInstance returns what an instance can still undo, most recent first.
//
// Newest first because compensation runs in reverse order: the last thing done
// is the first thing undone, and a list in the other order would have every
// caller reverse it.
func (r *compensatableActivityRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.CompensatableActivityModel, error) {
	if err := r.requireInstanceInTenant(ctx, instanceID); err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := compensatableactivity.New().
		Where(compensatableactivity.InstanceID.Eq(instanceID)).
		Order(compensatableactivity.CompletedAt.Desc()).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the compensatable activities: %w", err)
	}
	out := make([]models.CompensatableActivityModel, 0, len(rows))
	for _, row := range rows {
		activity, err := compensatableFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, activity)
	}
	return out, nil
}

// MarkCompensated records that an activity has been undone, so a second
// compensation does not undo it twice.
func (r *compensatableActivityRepository) MarkCompensated(ctx context.Context, id uuid.UUID) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	row, found, err := compensatableactivity.New().Where(compensatableactivity.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the compensatable activity: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such compensatable activity", apierr.ErrNotFound)
	}
	if err := r.requireInstanceInTenant(ctx, uuid.UUID(row.InstanceID)); err != nil {
		return err
	}
	mut := compensatableactivity.Mutate(row)
	mut.SetCompensated(true)
	if err := mut.Update(ctx, ex); err != nil {
		if errors.Is(err, runtime.ErrNoRow) {
			return fmt.Errorf("%w: no such compensatable activity", apierr.ErrNotFound)
		}
		return fmt.Errorf("could not mark the activity compensated: %w", err)
	}
	return nil
}

func compensatableFrom(row compensatableactivity.Row) (models.CompensatableActivityModel, error) {
	variables, err := sealedMapOf(row.Variables)
	if err != nil {
		return models.CompensatableActivityModel{}, fmt.Errorf("could not decode an activity's variables: %w", err)
	}
	return models.CompensatableActivityModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		InstanceID:         models.UUID(row.InstanceID),
		NodeID:             row.NodeID,
		CompensationNodeID: row.CompensationNodeID,
		Variables:          variables,
		CompletedAt:        row.CompletedAt,
		Compensated:        row.Compensated,
	}, nil
}
