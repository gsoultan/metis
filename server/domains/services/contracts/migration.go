package contracts

import (
	"context"

	"github.com/google/uuid"
)

// MigrationService defines the operations for migrating process instances.
type MigrationService interface {
	MigrateInstances(ctx context.Context, sourceDefID uuid.UUID, targetDefID uuid.UUID, nodeMapping map[string]string) error
}
