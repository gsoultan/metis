package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// WorkflowUserPager reads a project's directory a page at a time.
//
// Apart from ListWorkflowUsers, which answers everybody: the designer's
// picker and a sync both need the whole directory, while the People page
// needs a window of it and a count.
type WorkflowUserPager interface {
	// PageWorkflowUsers returns one page of a project's participants,
	// alphabetically by username, with how many there are in all. A search
	// keeps only those whose username, display name or email contains it.
	PageWorkflowUsers(ctx context.Context, projectID uuid.UUID, search string, page repocontracts.Pagination) (repocontracts.Page[entities.WorkflowUser], error)
}
