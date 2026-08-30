package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

// CompensatableActivityRepository persists and retrieves compensatable activity
// records for the Saga coordination pattern.
type CompensatableActivityRepository interface {
	// Create records a new compensatable activity.
	Create(ctx context.Context, m models.CompensatableActivityModel) (models.CompensatableActivityModel, error)

	// ListByInstance returns all uncompensated activities for the given instance,
	// ordered by completion time descending (most recent first) for reverse rollback.
	ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.CompensatableActivityModel, error)

	// MarkCompensated marks the activity as compensated so it is not rolled back again.
	MarkCompensated(ctx context.Context, id uuid.UUID) error
}
