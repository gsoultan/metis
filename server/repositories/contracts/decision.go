package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

// DecisionRepository defines the decision definition operations.
type DecisionRepository interface {
	Get(ctx context.Context, id uuid.UUID) (models.DecisionDefinitionModel, error)

	// GetByKey returns the highest version of a key within one project.
	//
	// Keys are unique per project, not per installation, so a key is only an
	// answer together with the project that owns it — the same shape as the
	// form repository's lookup.
	GetByKey(ctx context.Context, projectID uuid.UUID, key string) (models.DecisionDefinitionModel, error)
	GetByKeyAndVersion(ctx context.Context, projectID uuid.UUID, key string, version int) (models.DecisionDefinitionModel, error)
	List(ctx context.Context) ([]models.DecisionDefinitionModel, error)
	ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.DecisionDefinitionModel, error)

	// ListByProjectPaged returns one page of a project's decisions, for the
	// same reason definitions have one. A non-empty search keeps the ones whose
	// name or key contains it, ignoring case, so a list can be searched a page
	// at a time rather than by loading all of it.
	ListByProjectPaged(ctx context.Context, projectID uuid.UUID, search string, p Pagination) (Page[models.DecisionDefinitionModel], error)

	// ListLatestByProject returns one page of a project's decision keys, each
	// as its newest version and without the table columns, ordered by key: what
	// a picker or the dependency graph needs, and a small fraction of what the
	// full rows weigh.
	ListLatestByProject(ctx context.Context, projectID uuid.UUID, p Pagination) (Page[models.DecisionSummaryModel], error)

	// NextVersion proposes the version a new deployment of key should claim,
	// counted over the rows the (project_id, key, version) unique index covers.
	// It is a proposal: concurrent callers get the same number and the index
	// decides which one keeps it.
	NextVersion(ctx context.Context, projectID uuid.UUID, key string) (int, error)

	// Create stores a new version. There is no Update: a stored version is
	// what the instances that evaluated it were decided under, so it is never
	// changed — an edit is the key's next version.
	Create(ctx context.Context, definition models.DecisionDefinitionModel) error
	Delete(ctx context.Context, id uuid.UUID) error
}
