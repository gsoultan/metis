package impl

import (
	"context"
	"io"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// NewUnavailableWorkflowUserService stands in when there is no storm
// connection.
//
// Storm is PostgreSQL-only, so an installation on SQLite, MySQL or SQL Server
// has no participant directory until it moves. Answering with a plain refusal
// that says why is better than either of the alternatives: a nil service panics
// on the first request, and silently returning an empty list would look exactly
// like a project with nobody in it.
func NewUnavailableWorkflowUserService() servicecontracts.WorkflowUserService {
	return unavailableWorkflowUsers{}
}

type unavailableWorkflowUsers struct{}

const unavailableReason = "the participant directory needs PostgreSQL; this installation is on another database engine"

func (unavailableWorkflowUsers) ListWorkflowUsers(context.Context, uuid.UUID, int) ([]entities.WorkflowUser, error) {
	return nil, apierr.Invalidf("%s", unavailableReason)
}

func (unavailableWorkflowUsers) ImportWorkflowUsers(context.Context, uuid.UUID, io.Reader) (entities.ImportSummary, error) {
	return entities.ImportSummary{}, apierr.Invalidf("%s", unavailableReason)
}

func (unavailableWorkflowUsers) SyncWorkflowUsersFromHTTP(context.Context, uuid.UUID, string, string) (entities.ImportSummary, error) {
	return entities.ImportSummary{}, apierr.Invalidf("%s", unavailableReason)
}

func (unavailableWorkflowUsers) SyncWorkflowUsersFromPostgres(context.Context, uuid.UUID, string, string) (entities.ImportSummary, error) {
	return entities.ImportSummary{}, apierr.Invalidf("%s", unavailableReason)
}

func (unavailableWorkflowUsers) RemoveWorkflowUser(context.Context, uuid.UUID, uuid.UUID) error {
	return apierr.Invalidf("%s", unavailableReason)
}
