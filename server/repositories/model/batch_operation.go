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
	// Organization is the organization the request was for: the one whose
	// roles the requester is checked against at every step.
	Organization Organization

	Kind   string
	Reason string

	// The filter the instances were chosen by, as it was asked for.
	FilterStatus         string
	FilterDefinition     *ProcessDefinition
	FilterBusinessKey    string
	FilterNeedsAttention bool

	RequestedBy string
	// RequestedByUser is the account that asked, kept as requested_by_id.
	RequestedByUser User

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
	// The record of what was asked outlives everything it names.
	t.Col(&b.Organization).NoIndex().AcknowledgeNoFK("the record outlives the organization it was asked for")
	t.Col(&b.FilterDefinition).NoIndex().AcknowledgeNoFK("the filter as asked outlives the definition it named")
	t.Col(&b.RequestedByUser).Named("requested_by_id").NoIndex().AcknowledgeNoFK("the record outlives the account that asked")
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
	// A worker claims a running operation by taking it, and an instance's
	// result moves the operation's counts in the statement that records it.
	t.Takes()
	t.Tallies()
}

// BatchOperationItem is one instance of a batch operation and what came of
// acting on it.
type BatchOperationItem struct {
	storm.Model

	BatchOperation BatchOperation
	// Instance has no foreign key, by the ledger's rule: the worker must not
	// wait on a row the engine holds to record what it did.
	Instance ProcessInstance

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
	t.Col(&i.Instance).NoIndex().AcknowledgeNoFK("the worker must not wait on an instance row the engine holds")
	// One row per instance of an operation, read in that order by the worker
	// and by the list of results, which narrows by status.
	t.Index(&i.BatchOperation, &i.Instance).Unique().Named("ux_batch_operation_items_instance")
	t.Index(&i.BatchOperation, &i.Status, &i.Instance).Named("ix_batch_operation_items_status")
	// An operation's items are copied from the instances its filter
	// matches, in one statement, without reading them.
	t.InsertsPer(&i.Instance)
}
