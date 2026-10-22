package entities

import (
	"time"

	"github.com/google/uuid"
)

// BatchOperationKind is the act a batch operation makes on each instance.
type BatchOperationKind string

const (
	// BatchRetry resolves every open incident of each instance, so its failed
	// step runs again, as resolving one incident at a time does.
	BatchRetry BatchOperationKind = "retry"
	// BatchCancel cancels each instance where it stands, as an administrator's
	// cancel of one instance does: its tasks are withdrawn, its waiting work
	// let go, and the cancel written to its ledger and its trail.
	BatchCancel BatchOperationKind = "cancel"
)

// BatchOperationStatus is where a batch operation stands.
type BatchOperationStatus string

const (
	// BatchRunning is being worked through.
	BatchRunning BatchOperationStatus = "running"
	// BatchCompleted reached every instance it was asked for.
	BatchCompleted BatchOperationStatus = "completed"
	// BatchStopped was stopped before it was through, by a person or because
	// the one who asked may no longer make the act. The instances it had not
	// reached are skipped.
	BatchStopped BatchOperationStatus = "stopped"
)

// BatchItemStatus is what came of one instance of a batch operation.
type BatchItemStatus string

const (
	BatchItemPending   BatchItemStatus = "pending"
	BatchItemSucceeded BatchItemStatus = "succeeded"
	BatchItemFailed    BatchItemStatus = "failed"
	// BatchItemSkipped had nothing to act on — no incident to retry, an
	// instance already ended — or was not reached before the operation was
	// stopped.
	BatchItemSkipped BatchItemStatus = "skipped"
)

// MaxBatchOperationInstances is the most instances one operation may be asked
// for. A filter that matches more is refused with how many it matched, so the
// person narrows it rather than acting on a set larger than they can check.
const MaxBatchOperationInstances = 10_000

// MaxBatchItemDetailLength is how many characters of an item's reason are
// kept: the column is that wide.
const MaxBatchItemDetailLength = 1024

// BatchOperationFilter chooses the instances of a project an operation acts
// on: the instance list's filter, every field optional.
type BatchOperationFilter struct {
	Status         string
	DefinitionID   uuid.UUID
	BusinessKey    string
	NeedsAttention bool
}

// BatchOperationRequest is what a person asks for.
type BatchOperationRequest struct {
	ProjectID uuid.UUID
	Kind      BatchOperationKind
	Filter    BatchOperationFilter
	Reason    string
	// DryRun asks how many instances the filter matches, and nothing else.
	DryRun bool
}

// BatchOperation is an act asked for on every instance a filter matched, and
// how far it has got.
//
// No JSON or ORM tags: an endpoint maps it to a view, and a repository maps it
// to a row.
type BatchOperation struct {
	ID             uuid.UUID
	CreatedAt      time.Time
	ProjectID      uuid.UUID
	OrganizationID uuid.UUID

	Kind   BatchOperationKind
	Reason string
	Filter BatchOperationFilter

	RequestedBy   string
	RequestedByID uuid.UUID

	Status    BatchOperationStatus
	Total     int64
	Succeeded int64
	Failed    int64
	Skipped   int64

	FinishedAt *time.Time
	StoppedBy  string
	StopReason string
}

// Pending is how many instances the operation has still to reach.
func (b BatchOperation) Pending() int64 {
	return max(b.Total-b.Succeeded-b.Failed-b.Skipped, 0)
}

// BatchOperationPreview is what a dry run answers: how many instances the
// filter matches now, and whether that is more than one operation may take.
type BatchOperationPreview struct {
	Kind    BatchOperationKind
	Matched int64
	TooMany bool
}

// BatchOperationOutcome is the answer to a request: a preview, or the
// operation that was started.
type BatchOperationOutcome struct {
	Preview   BatchOperationPreview
	Operation *BatchOperation
}

// BatchItem is one instance of an operation and what came of it.
type BatchItem struct {
	InstanceID uuid.UUID
	Status     BatchItemStatus
	Detail     string
	SettledAt  *time.Time
}
