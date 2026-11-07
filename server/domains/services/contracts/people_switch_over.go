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

	// StrandedTasks lists the project's open tasks that nobody can act on:
	// held by somebody who is no longer an account or an active person, or
	// offered only to people and groups with nobody left who can take them.
	// A task names who may do it by name, so leaving, deactivation and a
	// deleted group all leave it waiting for ever unless somebody hands it
	// over; this is how they find out.
	StrandedTasks(ctx context.Context, projectID uuid.UUID) ([]entities.StrandedTask, error)

	// HeldTasks lists the project's open tasks one of its people holds: what
	// deactivating them has to decide about.
	HeldTasks(ctx context.Context, projectID, participantID uuid.UUID) ([]entities.HeldTask, error)

	// SetParticipantActive deactivates or reactivates one of the project's
	// people. Deactivating does with the open tasks they hold what work says:
	// keep them, put them back on offer, or hand them to one account, each as
	// a hand-over by actor with work's reason. The result names every task
	// still with them.
	SetParticipantActive(ctx context.Context, projectID, participantID uuid.UUID, active bool, work entities.HeldWork, actor string) (entities.ActivationResult, error)
}
