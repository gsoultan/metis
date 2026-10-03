package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// TaskDelegationReader answers "what did I delegate that has not come back":
// the owner's side of a delegation, which the list of their own tasks cannot
// show because the delegate holds them.
type TaskDelegationReader interface {
	// ListTasksDelegatedByPaged returns one page of the tasks owner delegated
	// that are still with their delegate, newest first.
	ListTasksDelegatedByPaged(ctx context.Context, owner string, page repocontracts.Pagination) (repocontracts.Page[entities.Task], error)
}
