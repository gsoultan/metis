package contracts

import (
	"context"
	"io"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// WorkflowUserService manages the people a project's processes can assign work
// to.
type WorkflowUserService interface {
	// ListWorkflowUsers returns a project's participants, alphabetically by
	// username: at most limit of them, or all of them when limit is zero.
	ListWorkflowUsers(ctx context.Context, projectID uuid.UUID, limit int) ([]entities.WorkflowUser, error)

	// ImportWorkflowUsers reads a CSV of participants into a project.
	//
	// A bad row is reported and the rest still imports. All-or-nothing on a
	// five-hundred-row file fails at row four hundred and leaves whoever ran it
	// to work out which rows landed — the answer being none, which they then
	// have to take on trust.
	ImportWorkflowUsers(ctx context.Context, projectID uuid.UUID, csv io.Reader) (entities.ImportSummary, error)

	// SyncWorkflowUsersFromHTTP reads a directory from a JSON endpoint.
	//
	// Through the shared egress guard, because the address is caller-supplied:
	// without it this is a request the server makes to anywhere the caller
	// names, its own metadata service included.
	SyncWorkflowUsersFromHTTP(ctx context.Context, projectID uuid.UUID, url, method string) (entities.ImportSummary, error)

	// SyncWorkflowUsersFromPostgres reads a directory from a query against
	// another database.
	//
	// The most powerful of the three: it runs caller-supplied SQL against a
	// caller-supplied database. Validating the query cannot make that safe, so
	// the control is that reaching this is administrative.
	SyncWorkflowUsersFromPostgres(ctx context.Context, projectID uuid.UUID, dsn, query string) (entities.ImportSummary, error)

	// RemoveWorkflowUser takes somebody out of a project's directory.
	//
	// Their open tasks stay where they are. A task names its assignee rather
	// than referencing them, so work in an inbox does not disappear because the
	// person left — it stays for whoever picks it up, which is what an operator
	// needs when somebody leaves mid-approval.
	RemoveWorkflowUser(ctx context.Context, projectID, id uuid.UUID) error
}
