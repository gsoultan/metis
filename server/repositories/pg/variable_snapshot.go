package pg

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/variablesnapshot"
)

type variableSnapshotRepository struct{ conn }

// NewVariableSnapshotRepository returns the store of what an instance's
// variables were at a moment.
func NewVariableSnapshotRepository(c *db.Conn) contracts.VariableSnapshotRepository {
	return &variableSnapshotRepository{conn{conn: c}}
}

// Create records one snapshot.
//
// Unscoped for the same reason an audit entry is: it is written on the path of
// the thing it describes, and a snapshot that could be refused would leave a
// step with no record of what it saw.
func (r *variableSnapshotRepository) Create(ctx context.Context, m models.VariableSnapshotModel) (models.VariableSnapshotModel, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return models.VariableSnapshotModel{}, err
	}
	variables, err := sealedJSONOf(m.Variables)
	if err != nil {
		return models.VariableSnapshotModel{}, fmt.Errorf("could not encode the snapshot's variables: %w", err)
	}

	ins := variablesnapshot.Create()
	if id := uuid.UUID(m.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetInstanceID(uuid.UUID(m.InstanceID))
	ins.SetVariables(variables)
	ins.SetCapturedAt(m.CapturedAt)
	setOrNullString(ins.SetNodeID, ins.SetNodeIDNull, m.NodeID)
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		return models.VariableSnapshotModel{}, fmt.Errorf("could not record the snapshot: %w", err)
	}
	return snapshotFrom(row)
}

// ListByInstance returns one instance's snapshots, oldest first.
//
// Scoped through the instance rather than directly: a snapshot has no project
// of its own, so the check is that the instance is one the caller may see.
func (r *variableSnapshotRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.VariableSnapshotModel, error) {
	if err := r.requireInstanceInTenant(ctx, instanceID); err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := variablesnapshot.New().
		Where(variablesnapshot.InstanceID.Eq(instanceID)).
		Order(variablesnapshot.CapturedAt.Asc()).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the snapshots: %w", err)
	}
	out := make([]models.VariableSnapshotModel, 0, len(rows))
	for _, row := range rows {
		snapshot, err := snapshotFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, snapshot)
	}
	return out, nil
}

func snapshotFrom(row variablesnapshot.Row) (models.VariableSnapshotModel, error) {
	variables, err := sealedMapOf(row.Variables)
	if err != nil {
		return models.VariableSnapshotModel{}, fmt.Errorf("could not decode a snapshot's variables: %w", err)
	}
	return models.VariableSnapshotModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		InstanceID: models.UUID(row.InstanceID),
		NodeID:     valueOr(row.NodeID),
		Variables:  variables,
		CapturedAt: row.CapturedAt,
	}, nil
}
