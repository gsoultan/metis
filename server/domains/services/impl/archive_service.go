package impl

import (
	"context"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// The page sizes of a project's archive, whatever a caller asks for.
const (
	defaultArchivePage = 50
	maxArchivePage     = 200
)

// archiveService reads the archive the retention sweep writes.
type archiveService struct {
	repo repositories.Repository
}

// NewArchiveService returns the archive's reader.
func NewArchiveService(repo repositories.Repository) contracts.ArchiveService {
	return &archiveService{repo: repo}
}

// ListArchivedInstances lists a page of a project's archive. A page is
// defaultArchivePage rows unless asked otherwise, and never more than
// maxArchivePage.
func (s *archiveService) ListArchivedInstances(ctx context.Context, projectID uuid.UUID, filter repocontracts.ArchiveFilter) ([]models.ArchivedInstanceModel, error) {
	if filter.Limit <= 0 {
		filter.Limit = defaultArchivePage
	}
	filter.Limit = min(filter.Limit, maxArchivePage)
	return s.repo.Archive().List(ctx, projectID, filter)
}

// GetArchivedInstance opens one archived instance of the caller's.
func (s *archiveService) GetArchivedInstance(ctx context.Context, instanceID uuid.UUID) (models.ArchivedInstanceModel, models.ArchiveRecord, error) {
	return s.repo.Archive().Get(ctx, instanceID)
}
