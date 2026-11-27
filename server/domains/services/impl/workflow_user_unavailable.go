package impl

import (
	"context"
	"io"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
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

func (unavailableWorkflowUsers) PageWorkflowUsers(_ context.Context, _ uuid.UUID, _ string, page repocontracts.Pagination) (repocontracts.Page[entities.WorkflowUser], error) {
	return repocontracts.NewPage([]entities.WorkflowUser{}, 0, page), apierr.Invalidf("%s", unavailableReason)
}

func (unavailableWorkflowUsers) FindWorkflowUserByExternalID(context.Context, uuid.UUID, string) (entities.WorkflowUser, error) {
	return entities.WorkflowUser{}, apierr.Invalidf("%s", unavailableReason)
}

func (unavailableWorkflowUsers) ListWorkflowGroups(context.Context, uuid.UUID) ([]entities.WorkflowGroup, error) {
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

func (unavailableWorkflowUsers) ImportDirectory(context.Context, uuid.UUID, entities.DirectoryImport) (entities.ImportSummary, error) {
	return entities.ImportSummary{}, apierr.Invalidf("%s", unavailableReason)
}

func (unavailableWorkflowUsers) CheckDirectoryImport(context.Context, uuid.UUID, entities.DirectoryImport) (entities.ImportCheck, error) {
	return entities.ImportCheck{}, apierr.Invalidf("%s", unavailableReason)
}

func (unavailableWorkflowUsers) RemoveWorkflowUser(context.Context, uuid.UUID, uuid.UUID) error {
	return apierr.Invalidf("%s", unavailableReason)
}

func (unavailableWorkflowUsers) SetWorkflowUserPassword(context.Context, uuid.UUID, uuid.UUID, string) error {
	return apierr.Invalidf("%s", unavailableReason)
}
