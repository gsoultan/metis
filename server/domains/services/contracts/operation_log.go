package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// OperationLogService reads the audit log: who suspended, waived, migrated,
// reassigned, held or approved what, across the caller's organization.
//
// It reads the trail the acts already write and changes nothing. Reading it
// is an administrator's of the organization: the log names every person's
// acts on every instance, which no one project's view shows.
type OperationLogService interface {
	// ListOperations returns a page of the log, newest first.
	ListOperations(ctx context.Context, query entities.OperationQuery) (entities.OperationPage, error)
}
