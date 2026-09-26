package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

// DecisionRepository defines the decision definition operations.
type DecisionRepository interface {
	Get(ctx context.Context, id uuid.UUID) (models.DecisionDefinitionModel, error)

	// GetLiveByKey returns the version of a key its release timeline names as
	// live: what an evaluation that names no version reads. Not found when no
	// version is live — never "the highest", which would put into force a
	// version nobody chose.
	//
	// Keys are unique per project, not per installation, so a key is only an
	// answer together with the project that owns it — the same shape as the
	// form repository's lookup.
	GetLiveByKey(ctx context.Context, projectID uuid.UUID, key string) (models.DecisionDefinitionModel, error)
	GetByKeyAndVersion(ctx context.Context, projectID uuid.UUID, key string, version int) (models.DecisionDefinitionModel, error)

	// GetRelease is the newest entry on a key's release timeline whose moment
	// has come. Not found is a normal answer: nothing has been made live.
	GetRelease(ctx context.Context, projectID uuid.UUID, key string) (models.DecisionReleaseModel, error)
	List(ctx context.Context) ([]models.DecisionDefinitionModel, error)
	ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.DecisionDefinitionModel, error)

	// ListByProjectPaged returns one page of a project's decisions, for the
	// same reason definitions have one. A non-empty search keeps the ones whose
	// name or key contains it, ignoring case, so a list can be searched a page
	// at a time rather than by loading all of it.
	ListByProjectPaged(ctx context.Context, projectID uuid.UUID, search string, p Pagination) (Page[models.DecisionDefinitionModel], error)

	// ListKeysByProject returns one page of a project's decision keys, one
	// row each without the table columns, ordered by key: the live version,
	// the newest, and when either last changed. What the decision list, a
	// picker or the dependency graph needs, and a small fraction of what the
	// full rows weigh. A non-empty search keeps the keys whose key, or shown
	// version's name, contains it, ignoring case.
	ListKeysByProject(ctx context.Context, projectID uuid.UUID, search string, p Pagination) (Page[models.DecisionSummaryModel], error)

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

	// MakeLive records that from now on the key's live version is version: one
	// more entry on its release timeline, effective now.
	MakeLive(ctx context.Context, projectID uuid.UUID, key string, version int) error

	// ListVersionsByKey returns every stored version of one key, newest first.
	ListVersionsByKey(ctx context.Context, projectID uuid.UUID, key string) ([]models.DecisionDefinitionModel, error)

	// LockVersion reads one version and holds a row lock on it until the
	// transaction around ctx ends. Making a version live and deleting it both
	// take it first, so neither can act on what the other is about to change.
	LockVersion(ctx context.Context, projectID uuid.UUID, key string, version int) (models.DecisionDefinitionModel, error)
}
