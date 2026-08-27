package contracts

import (
	"context"

	"github.com/gsoultan/gobpm/server/repositories/models"

	"github.com/google/uuid"
)

// DefinitionRepository defines the process definition operations.
type DefinitionRepository interface {
	Get(ctx context.Context, id uuid.UUID) (models.ProcessDefinitionModel, error)
	GetByKey(ctx context.Context, key string) (models.ProcessDefinitionModel, error)
	GetByKeyAndVersion(ctx context.Context, key string, version int) (models.ProcessDefinitionModel, error)
	List(ctx context.Context) ([]models.ProcessDefinitionModel, error)

	// ScanWithGraphs walks the caller's definitions with their node and flow
	// graphs hydrated, one batch at a time. List deliberately projects those
	// columns away — a list page does not need them — so a scan over what
	// definitions *contain* (the javascript-conditions worklist) must ask for
	// them explicitly, and takes a callback so peak memory is one batch rather
	// than every version of every process the installation has ever held.
	ScanWithGraphs(ctx context.Context, visit func([]models.ProcessDefinitionModel) error) error

	ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.ProcessDefinitionModel, error)

	// ListByProjectPaged returns one page of a project's definitions. The
	// unpaged call above returns every version of every process a project has
	// ever had, which is a list that only grows.
	ListByProjectPaged(ctx context.Context, projectID uuid.UUID, p Pagination) (Page[models.ProcessDefinitionModel], error)

	// NextVersion proposes the version a new deployment of key should claim,
	// counted over the rows the (project_id, key, version) unique index covers.
	// It is a proposal: concurrent callers get the same number and the index
	// decides which one keeps it.
	NextVersion(ctx context.Context, projectID uuid.UUID, key string) (int, error)

	Create(ctx context.Context, definition models.ProcessDefinitionModel) error
	Delete(ctx context.Context, id uuid.UUID) error
}
