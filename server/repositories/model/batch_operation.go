package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// BatchOperation is one act asked for on every instance a filter matched in a
// project — retry them, or cancel them — and how far it has got.
//
// The instances are fixed when it is asked for: the filter is kept to show
// what was asked, and BatchOperationItem rows are what is worked through. The
// row is the record of who asked, when and why, and is never swept.
type BatchOperation struct {
	storm.Model

	Project Project
	// OrganizationID is the organization the request was for: the one whose
	// roles the requester is checked against at every step.
	OrganizationID storm.UUID

	Kind   string
	Reason string

	// The filter the instances were chosen by, as it was asked for.
	FilterStatus         string
	FilterDefinitionID   *storm.UUID
	FilterBusinessKey    string
	FilterNeedsAttention bool

	RequestedBy   string
	RequestedByID storm.UUID

	Status    string
	Total     int64
	Succeeded int64
	Failed    int64
	Skipped   int64

	// LeaseExpires is when a worker that took the operation is presumed gone.
	LeaseExpires *time.Time
	FinishedAt   *time.Time
	// StoppedBy is who stopped it before it was through, and StopReason why.
	StoppedBy  string
	StopReason string
}

func (b *BatchOperation) Schema(t *storm.Table) {
	t.Name("batch_operations")
	t.Col(&b.Kind).Size(16)
	t.Col(&b.FilterStatus).Size(32)
	t.Col(&b.FilterBusinessKey).Size(255)
	t.Col(&b.RequestedBy).Size(255)
	t.Col(&b.Status).Size(16)
	t.Col(&b.StoppedBy).Size(255)
	// A project's operations, newest first.
	t.Index(&b.Project, &b.CreatedAt, &b.ID).Named("ix_batch_operations_project")
	// The worker's claim: the running ones, oldest first. Partial, so it stays
	// the size of what is running rather than of the history. Spelled as
	// PostgreSQL prints it back, as the outgoing webhooks' is.
	t.Index(&b.CreatedAt).Where("((status)::text = 'running'::text)").Named("ix_batch_operations_running")
}

// BatchOperationItem is one instance of a batch operation and what came of
// acting on it.
type BatchOperationItem struct {
	storm.Model

	BatchOperation BatchOperation
	// InstanceID is a plain column, by the ledger's rule: the worker must not
	// wait on a row the engine holds to record what it did.
	InstanceID storm.UUID

	Status string
	// Detail is why an instance failed or was skipped, in words somebody can
	// act on.
	Detail    string
	SettledAt *time.Time
}

func (i *BatchOperationItem) Schema(t *storm.Table) {
	t.Name("batch_operation_items")
	t.Col(&i.Status).Size(16)
	t.Col(&i.Detail).Size(1024)
	// One row per instance of an operation, read in that order by the worker
	// and by the list of results, which narrows by status.
	t.Index(&i.BatchOperation, &i.InstanceID).Unique().Named("ux_batch_operation_items_instance")
	t.Index(&i.BatchOperation, &i.Status, &i.InstanceID).Named("ix_batch_operation_items_status")
}
