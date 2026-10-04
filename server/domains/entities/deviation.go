package entities

import (
	"time"

	"github.com/google/uuid"
)

// MaxDeviationReasonLength is the longest reason a person may give for a
// deviation, in characters. Longer is an essay, and the ledger is read row by
// row.
const MaxDeviationReasonLength = 2000

// Deviation is one act on one instance that its process did not decide: a task
// waived, an instance cancelled or held, work handed to somebody the model did
// not name.
//
// It is what an auditor reads to answer "what was done to this instance that
// the diagram does not say", and what the instance's own timeline interleaves
// with the audit trail.
//
// Before and After hold only what changed, in sections named variables, tasks,
// instance and incident. Details is for what does not fit a before and after
// and is not business data.
//
// No JSON or ORM tags: an endpoint maps it to a view, and a repository maps it
// to a row.
type Deviation struct {
	ID         uuid.UUID
	CreatedAt  time.Time
	Project    *Project
	Instance   *ProcessInstance
	Definition *ProcessDefinition

	Kind   DeviationKind
	Scope  DeviationScope
	Origin DeviationOrigin
	Status DeviationStatus

	Node        *Node
	Task        *Task
	IterationID string

	Actor   string
	ActorID uuid.UUID
	Reason  string

	Before, After, Details map[string]any

	// RunID groups the rows one act wrote, so a migration that skipped three
	// tasks reads as one act with three rows.
	RunID uuid.UUID
	// AuditEntryID is the audit entry that narrates the same act.
	AuditEntryID uuid.UUID
	// VisitKey makes the row idempotent: one act on one visit is one row, however
	// often the request is retried.
	VisitKey string

	// RequestID, ApprovedBy, ApprovedByID and DecidedAt belong to a deviation
	// that a second person must approve.
	RequestID    uuid.UUID
	ApprovedBy   string
	ApprovedByID uuid.UUID
	DecidedAt    *time.Time
}
