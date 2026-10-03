package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// DeviationReader reads the deviation ledger.
type DeviationReader interface {
	// ListInstanceDeviations returns every deviation an instance has, oldest
	// first. An instance of another organization has none that the caller can see.
	ListInstanceDeviations(ctx context.Context, instanceID uuid.UUID) ([]entities.Deviation, error)
}
