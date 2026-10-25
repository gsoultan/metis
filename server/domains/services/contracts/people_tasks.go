package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// PeopleTaskService is the work a signed-in person does: the tasks processes
// gave them, in their own project, and nothing else.
//
// The rules are the platform's — assignee, candidate users, candidate groups,
// separation of duties, the form's declared variables — checked against the
// person as the People directory has them. Every method refuses a task in
// another project as not found.
type PeopleTaskService interface {
	// ListPersonTasks is their inbox: what they hold, and the unclaimed tasks
	// offered to them or to one of their groups.
	ListPersonTasks(ctx context.Context, person entities.Person, page repocontracts.Pagination) (repocontracts.Page[entities.Task], error)

	// GetPersonTask returns one task, if it is theirs or offered to them.
	GetPersonTask(ctx context.Context, person entities.Person, id uuid.UUID) (entities.Task, error)

	// ClaimPersonTask takes an unclaimed task offered to them.
	ClaimPersonTask(ctx context.Context, person entities.Person, id uuid.UUID) error

	// ReleasePersonTask gives back a task they claimed from a pool, for its
	// candidates to claim again. A task given to them by name stays theirs.
	ReleasePersonTask(ctx context.Context, person entities.Person, id uuid.UUID) error

	// CompletePersonTask completes a task they hold, or an unclaimed one
	// offered to them, with the variables its form declares.
	CompletePersonTask(ctx context.Context, person entities.Person, id uuid.UUID, vars map[string]any) error
}
