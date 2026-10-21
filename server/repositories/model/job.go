package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// Job is queued work the engine owes: a timer that must fire, a service call
// that must be made.
//
// The claim query is "pending jobs whose time has come, oldest first", run
// several times a second forever, which is why status and next_run_at are
// indexed together rather than separately — the planner picks one single-column
// index and filters the rest, and that scan grows with every job ever completed.
type Job struct {
	storm.Model

	Instance   ProcessInstance
	Definition ProcessDefinition

	// OrganizationID is whose work this is, copied from the instance's project
	// when the job is made, so the claim can share the workers between
	// organizations. Nil on a job made before migration 44. No foreign key: the
	// claim reads it, and nothing is held up by an organization's row.
	OrganizationID *storm.UUID

	NodeID      string
	IterationID *string

	Type   JobType
	Status JobStatus

	// A claimed job names its worker and when the claim lapses, so work is
	// recovered if that worker dies rather than being held forever.
	LockedBy    *string
	LockExpires *time.Time

	Payload storm.JSON

	Retries          int
	MaxRetries       int
	RepeatsRemaining int
	NextRunAt        time.Time
	LastError        *string

	DeletedAt *time.Time
}

func (j *Job) Schema(t *storm.Table) {
	t.Col(&j.NodeID).Size(255)
	t.Col(&j.Type).Size(64)
	t.Col(&j.Status).Size(32)
	// The claim query's index: the predicate and the order in one.
	t.Index(&j.Status, &j.NextRunAt)
	// The fair claim's: each organization's due jobs, oldest first, as one
	// range.
	t.Index(&j.Status, &j.OrganizationID, &j.NextRunAt).Named("ix_jobs_claim_by_organization")
	t.Col(&j.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&j.DeletedAt)
}
