package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// WorkflowUserRepository stores the people a project's processes can assign
// work to.
//
// The first repository written against storm rather than GORM. It could be:
// the table is new, so there is no GORM implementation to keep in step with it
// while the rest of the port happens.
type WorkflowUserRepository interface {
	// ListByProject returns a project's participants, alphabetically by
	// username: at most limit of them, or all of them when limit is zero.
	ListByProject(ctx context.Context, projectID uuid.UUID, limit int) ([]entities.WorkflowUser, error)
	GetByUsername(ctx context.Context, projectID uuid.UUID, username string) (entities.WorkflowUser, error)

	// Upsert writes one participant, replacing the row with the same username
	// in the same project if there is one.
	//
	// Returns whether the row was created, so an import can report how many
	// people are new against how many it overwrote.
	Upsert(ctx context.Context, participant entities.WorkflowUser) (created bool, err error)

	// EnsureGroup returns the named group for a project, creating it if the
	// project has no group of that name yet. Importing a directory that
	// mentions a team should not fail because nobody created the team first.
	EnsureGroup(ctx context.Context, projectID uuid.UUID, name string) (group entities.WorkflowGroup, created bool, err error)

	// AddToGroup puts a participant in a group. Adding somebody who is already
	// in it is not an error — an import that runs twice must not fail the
	// second time.
	AddToGroup(ctx context.Context, participantID, groupID uuid.UUID) error

	// Delete removes somebody from a project's directory.
	//
	// The row is marked, not destroyed, and it keeps its key: an import naming
	// them again reinstates the same person rather than creating a second one,
	// so the tasks they completed and the groups they were in are still theirs.
	//
	// Their open tasks are untouched, because a task names its assignee rather
	// than referencing them — see TestATaskNamesItsAssigneeWithoutReferencingThem.
	// That is deliberate: work in somebody's inbox does not evaporate because
	// they left, it stays for whoever picks it up.
	Delete(ctx context.Context, id uuid.UUID) error
}
