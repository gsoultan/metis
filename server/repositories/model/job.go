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

	// Organization is whose work this is, copied from the instance's project
	// when the job is made, so the claim can share the workers between
	// organizations. Nil on a job made before migration 44. No foreign key: the
	// claim reads it, and nothing is held up by an organization's row.
	Organization *Organization

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

	// Incidents is the inverse of Incident.Job: no column here. It is what
	// lets the retention sweep leave the failed jobs an open incident names
	// in the same statement that removes the rest.
	Incidents []Incident
}

func (j *Job) Schema(t *storm.Table) {
	// GORM's defaults, kept. See fill.go.
	t.Col(&j.Retries).Default(zero)
	t.Col(&j.MaxRetries).Default(zero)
	t.Col(&j.RepeatsRemaining).Default(zero)
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&j.NodeID).FillNulls(blank)
	t.Col(&j.Type).FillNulls(blank)
	t.Col(&j.Status).FillNulls(blank)
	t.Col(&j.Payload).ConvertFrom(storm.JSONText).FillNulls(jsonNull)
	t.Col(&j.Organization).NoIndex().AcknowledgeNoFK("the claim must not be held up by an organization's row")
	// No size: a step's id is the definition's, and nothing at deploy limits how long it is.
	t.Col(&j.Type).Size(64)
	t.Col(&j.Status).Size(32)
	// A claim that takes over a job whose worker died counts as an attempt;
	// the guard reads the status the claim replaces, in the same UPDATE.
	t.Col(&j.Retries).IncWhen(&j.Status)
	// The claim query's index: the predicate and the order in one.
	t.Index(&j.Status, &j.NextRunAt)
	// The sweeps that ask what is due regardless of state.
	t.Index(&j.NextRunAt).Named("idx_jobs_next_run_at")
	// The fair claim's: each organization's due jobs, oldest first, as one
	// range.
	t.Index(&j.Status, &j.Organization, &j.NextRunAt).Named("ix_jobs_claim_by_organization")
	// The claim deals the due jobs out by organization, a turn each, finding
	// the organizations a seek at a time along that index (AllTurns).
	t.TakesTurns(&j.Organization)
	t.Col(&j.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&j.DeletedAt)
	// The retention sweep purges a batch at a time (HardDeleteBatch).
	t.Takes()
}
