package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

// VariableSnapshotRepository persists and retrieves variable snapshot records.
type VariableSnapshotRepository interface {
	// Create stores a new variable snapshot.
	Create(ctx context.Context, m models.VariableSnapshotModel) (models.VariableSnapshotModel, error)

	// ListByInstance returns all snapshots for the given instance ordered by captured_at ASC.
	ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.VariableSnapshotModel, error)
}
