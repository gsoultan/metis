package contracts

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// ErrBatchOperationOutsideTransaction is what Create answers when no
// transaction is open: an operation and its items are written together or not
// at all, so the worker never finds one with half its instances.
var ErrBatchOperationOutsideTransaction = errors.New("a batch operation is recorded inside a transaction, and none is open")

// BatchOperationRepository stores acts asked for on many instances at once,
// and what came of each instance.
//
// What a person reads and asks for — counting, creating, reading, listing and
// stopping — is scoped to the caller's organization: an operation of somebody
// else's project is answered as one that is not there. What the worker does —
// claiming, reading what is left, settling and finishing — spans every
// organization and is system work only.
type BatchOperationRepository interface {
	// CountMatching counts the project's instances the filter matches now.
	CountMatching(ctx context.Context, projectID uuid.UUID, filter entities.BatchOperationFilter) (int64, error)
	// CountRunning counts the project's operations still being worked through.
	CountRunning(ctx context.Context, projectID uuid.UUID) (int64, error)
	// Create records an operation and an item for every instance its filter
	// matches, oldest first and at most limit, inside the caller's
	// transaction, and answers it with its total.
	Create(ctx context.Context, operation entities.BatchOperation, limit int) (entities.BatchOperation, error)
	// Get reads one operation.
	Get(ctx context.Context, id uuid.UUID) (entities.BatchOperation, error)
	// ListByProject lists a project's operations, newest first, at most limit.
	ListByProject(ctx context.Context, projectID uuid.UUID, limit int64) ([]entities.BatchOperation, error)
	// Items reads a page of an operation's instances in the order they are
	// worked through, narrowed to one status unless it is empty.
	Items(ctx context.Context, id uuid.UUID, status entities.BatchItemStatus, page Pagination) (Page[entities.BatchItem], error)
	// Stop ends a running operation before it is through: the instances it
	// has not reached are skipped, with the reason. An operation already over
	// is answered as it is.
	Stop(ctx context.Context, id uuid.UUID, by, reason string, now time.Time) (entities.BatchOperation, error)

	// ClaimRunning takes the oldest running operation no worker holds, for
	// lease. found is false when there is none.
	ClaimRunning(ctx context.Context, now time.Time, lease time.Duration) (operation entities.BatchOperation, found bool, err error)
	// PendingInstances lists up to limit of the instances an operation has
	// still to reach, in the order they are worked through.
	PendingInstances(ctx context.Context, id uuid.UUID, limit int) ([]uuid.UUID, error)
	// Settle records what came of one instance and counts it, if the
	// instance was still pending, or was skipped and the act did something:
	// a stop that came while the instance was being acted on does not hide
	// that it was. settled is false otherwise — another worker got there
	// first, or the act would only have skipped it.
	Settle(ctx context.Context, id uuid.UUID, item entities.BatchItem) (settled bool, err error)
	// Finish marks a running operation completed once it has no instance
	// left to reach, and lets go of it either way.
	Finish(ctx context.Context, id uuid.UUID, now time.Time) error
}
