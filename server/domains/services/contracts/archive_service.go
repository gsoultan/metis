package contracts

import (
	"context"

	"github.com/google/uuid"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// ArchiveService reads the finished instances the retention sweep archived.
// Read-only: an archived instance is a record, and nothing acts on it. Open
// to whoever may read the project's instances.
type ArchiveService interface {
	// ListArchivedInstances lists a project's archived instances, most
	// recently finished first.
	ListArchivedInstances(ctx context.Context, projectID uuid.UUID, filter repocontracts.ArchiveFilter) ([]models.ArchivedInstanceModel, error)
	// GetArchivedInstance opens one archived instance's record.
	GetArchivedInstance(ctx context.Context, instanceID uuid.UUID) (models.ArchivedInstanceModel, models.ArchiveRecord, error)
}
