package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// DefinitionService defines the process definition operations.
type DefinitionService interface {
	// CreateDefinition deploys a new version and makes it live. It is
	// DeployDefinition with promote set, kept because that is what every caller
	// that does not care about staging means.
	CreateDefinition(ctx context.Context, def *entities.ProcessDefinition) (uuid.UUID, error)

	// DeployDefinition deploys a new version, promoting it to live only when
	// asked.
	//
	// Staging is not a no-op: with no release row the live version is defined as
	// the highest, so staging has to pin the *current* live version before the
	// new row makes it stop being the highest. Otherwise "stage" would promote.
	//
	// Instances already running are unaffected either way. They pin their
	// definition by ID and drain on the version they started on.
	DeployDefinition(ctx context.Context, def *entities.ProcessDefinition, promote bool) (uuid.UUID, error)

	// PromoteDefinitionVersion makes one existing version the one new instances
	// start on, effective now. Running instances are not touched — there is no
	// migration, by design; see AGENTS.md on why moving a token between graphs is
	// unsafe.
	PromoteDefinitionVersion(ctx context.Context, projectID uuid.UUID, key string, version int) error

	// ScheduleDefinitionVersion arranges for a version to take over at a given
	// time. The time must be in the future; a past one is refused rather than
	// treated as "now", because those are different acts.
	//
	// Nothing runs at that moment. The live version is resolved from the release
	// timeline and the clock on every read, so the cutover happens by the time
	// arriving — it cannot be missed by a replica that was restarting, and needs
	// no lock, leader or catch-up pass.
	ScheduleDefinitionVersion(ctx context.Context, projectID uuid.UUID, key string, version int, activateAt time.Time) error

	// CancelScheduledVersion drops a cutover that has not happened yet. One that
	// already has is history and cannot be cancelled.
	CancelScheduledVersion(ctx context.Context, projectID uuid.UUID, releaseID uuid.UUID) error

	// ListDefinitionVersions returns every version deployed under one key with
	// its live flag and instance counts, newest first.
	ListDefinitionVersions(ctx context.Context, projectID uuid.UUID, key string) ([]entities.DefinitionVersionStatus, error)

	// ListLiveVersions maps each process key in a project to the version new
	// instances start on.
	//
	// One call for the whole project, because the page that needs it renders one
	// row per key and the alternative is a query per row. Keys that have never
	// been promoted are absent, and the caller resolves those the same way the
	// engine does — the highest version.
	ListLiveVersions(ctx context.Context, projectID uuid.UUID) (map[string]int, error)
	ListDefinitions(ctx context.Context, projectID uuid.UUID) ([]*entities.ProcessDefinition, error)

	// ListDefinitionsPaged returns one page of a project's definitions.
	ListDefinitionsPaged(ctx context.Context, projectID uuid.UUID, page repocontracts.Pagination) (repocontracts.Page[*entities.ProcessDefinition], error)
	GetDefinition(ctx context.Context, id uuid.UUID) (*entities.ProcessDefinition, error)
	DeleteDefinition(ctx context.Context, id uuid.UUID) error
	ExportDefinition(ctx context.Context, id uuid.UUID) ([]byte, error)
	ImportDefinition(ctx context.Context, projectID uuid.UUID, xml []byte) (uuid.UUID, error)

	// ListJavaScriptConditions reports every stored `js:` condition the caller
	// can see — the worklist for the javascript-conditions flag, which refuses
	// them by default.
	ListJavaScriptConditions(ctx context.Context) ([]entities.JavaScriptConditionUsage, error)

	// ListScriptTasks reports every script task the caller can see. An
	// inventory rather than a worklist: nothing here is refused, and it exists
	// so that the script sandbox's unbounded-memory gap can be sized before it
	// is fixed. See entities.ScriptTaskUsage.
	ListScriptTasks(ctx context.Context) ([]entities.ScriptTaskUsage, error)
}
