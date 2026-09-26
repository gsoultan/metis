package contracts

import (
	"context"
	"time"

	"github.com/gsoultan/metis/server/repositories/models"

	"github.com/google/uuid"
)

// DefinitionRepository defines the process definition operations.
type DefinitionRepository interface {
	Get(ctx context.Context, id uuid.UUID) (models.ProcessDefinitionModel, error)
	// GetByKey returns the version new instances start on: whichever
	// process_definition_releases names, or the highest when it names none.
	GetByKey(ctx context.Context, key string) (models.ProcessDefinitionModel, error)

	// GetLatestByKey returns the highest version regardless of which is live.
	// Once a version can be staged, "newest" and "live" are different questions.
	GetLatestByKey(ctx context.Context, key string) (models.ProcessDefinitionModel, error)

	GetByKeyAndVersion(ctx context.Context, key string, version int) (models.ProcessDefinitionModel, error)

	// GetLiveByProjectKey and GetByProjectKeyAndVersion are the key lookups the
	// engine uses. A process key is unique per project, not per organization, so
	// resolving by key alone is ambiguous the moment two projects in one tenant
	// share one — and the ambiguity decides which model runs. The engine always
	// knows the project.
	GetLiveByProjectKey(ctx context.Context, projectID uuid.UUID, key string) (models.ProcessDefinitionModel, error)
	GetByProjectKeyAndVersion(ctx context.Context, projectID uuid.UUID, key string, version int) (models.ProcessDefinitionModel, error)

	// GetRelease reports which version of key is live for one project. A missing
	// row is a normal answer meaning nobody has chosen, not a failure.
	//
	// Project-scoped rather than key-only: a key is unique per project, so the
	// key alone would answer with another project's choice.
	GetRelease(ctx context.Context, projectID uuid.UUID, key string) (models.ProcessDefinitionReleaseModel, error)

	// ScheduleRelease adds a timeline entry: from activateAt, key runs version.
	// A time already past is an immediate promotion; a future one is an arranged
	// cutover. Upserts so two administrators naming the same instant do not
	// report a duplicate key to whoever loses the race.
	ScheduleRelease(ctx context.Context, projectID uuid.UUID, key string, version int, activateAt time.Time) error

	// ListReleasesForKey returns one key's whole release timeline, newest first,
	// including cutovers that have not happened yet.
	ListReleasesForKey(ctx context.Context, projectID uuid.UUID, key string) ([]models.ProcessDefinitionReleaseModel, error)

	// DeleteScheduledRelease cancels a cutover that has not happened yet. It
	// re-checks that condition itself, because one that has since taken effect is
	// history and deleting it would rewrite which version has been in force.
	DeleteScheduledRelease(ctx context.Context, projectID uuid.UUID, id uuid.UUID) error

	// ListReleases returns every key's live version for one project, so a list
	// page can mark them in one query rather than one per row.
	ListReleases(ctx context.Context, projectID uuid.UUID) ([]models.ProcessDefinitionReleaseModel, error)

	// ListVersionsByKey returns every version deployed under one key, newest
	// first.
	ListVersionsByKey(ctx context.Context, projectID uuid.UUID, key string) ([]models.ProcessDefinitionModel, error)
	List(ctx context.Context) ([]models.ProcessDefinitionModel, error)

	// ScanWithGraphs walks the caller's definitions with their node and flow
	// graphs hydrated, one batch at a time. List deliberately projects those
	// columns away — a list page does not need them — so a scan over what
	// definitions *contain* (the javascript-conditions worklist) must ask for
	// them explicitly, and takes a callback so peak memory is one batch rather
	// than every version of every process the installation has ever held.
	ScanWithGraphs(ctx context.Context, visit func([]models.ProcessDefinitionModel) error) error

	// ScanProjectWithGraphs is ScanWithGraphs over one project, newest version
	// first: every version of every process it has deployed, a batch at a time.
	ScanProjectWithGraphs(ctx context.Context, projectID uuid.UUID, visit func([]models.ProcessDefinitionModel) error) error

	// ListKeysByProject returns the key of every process the project has, once
	// each, however many versions each has.
	ListKeysByProject(ctx context.Context, projectID uuid.UUID) ([]string, error)

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
