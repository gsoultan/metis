package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// VariableHistoryReader queries the variable snapshot history.
type VariableHistoryReader interface {
	// ListSnapshots returns all snapshots for the given instance, ordered by capture time ascending.
	ListSnapshots(ctx context.Context, instanceID uuid.UUID) ([]entities.VariableSnapshot, error)

	// GetVariableHistory returns the value history of a single variable across all snapshots.
	GetVariableHistory(ctx context.Context, instanceID uuid.UUID, variableName string) ([]entities.VariableSnapshot, error)
}
