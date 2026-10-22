package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// BatchOperationService retries or cancels every instance a filter matched in
// a project, in the background, and keeps what came of each.
//
// Asking for one is an operator's for a retry and an administrator's for a
// cancel, as it is for one instance; each instance is acted on as the person
// who asked, whose standing is read again before every few instances, so an
// operation stops when its requester may no longer make the act. Reading and
// stopping operations is an operator's of the project's organization.
type BatchOperationService interface {
	// StartBatchOperation answers how many instances a request's filter
	// matches when it is a dry run, and otherwise records the operation, with
	// those instances fixed, for the worker to work through.
	StartBatchOperation(ctx context.Context, request entities.BatchOperationRequest) (entities.BatchOperationOutcome, error)
	// BatchOperation reads one operation and how far it has got.
	BatchOperation(ctx context.Context, id uuid.UUID) (entities.BatchOperation, error)
	// ListBatchOperations lists a project's operations, newest first.
	ListBatchOperations(ctx context.Context, projectID uuid.UUID) ([]entities.BatchOperation, error)
	// BatchOperationItems reads a page of what came of an operation's
	// instances, narrowed to one status unless it is empty.
	BatchOperationItems(ctx context.Context, id uuid.UUID, status entities.BatchItemStatus, page repocontracts.Pagination) (repocontracts.Page[entities.BatchItem], error)
	// StopBatchOperation stops an operation before it is through; the
	// instances it has not reached are skipped.
	StopBatchOperation(ctx context.Context, id uuid.UUID, reason string) (entities.BatchOperation, error)
	// StartBatchOperations starts the worker that works through them.
	StartBatchOperations(ctx context.Context)
}
