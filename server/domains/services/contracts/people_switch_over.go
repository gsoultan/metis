package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// PeopleSwitchOverService finds the names a project's work uses that its
// People directory does not have, and fills them from the organization's
// Users and groups, before Users stop doing tasks.
type PeopleSwitchOverService interface {
	// DirectoryGaps lists every name an open task or a live process model of
	// the project uses, as assignee, candidate user or candidate group, that
	// is not a person or a People group of the project. Expressions are left
	// out: who they name is known only when a task is made.
	DirectoryGaps(ctx context.Context, projectID uuid.UUID) ([]entities.DirectoryGap, error)

	// FillDirectoryGaps adds the named gaps to the project's People: a person
	// from the User account of that name, with no password; a group from the
	// organization's group of that name, with its members. A name that is not
	// a gap, or has nothing to be made from, is skipped and reported.
	FillDirectoryGaps(ctx context.Context, projectID uuid.UUID, people, groups []string) (entities.DirectoryGapImport, error)
}
