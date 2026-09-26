package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// ServiceCall records an outbound call the engine made on a process's behalf.
//
// It exists so a retry does not repeat a call that already happened. The
// identity is (instance, node, iteration, job) — the same visit to the same step
// of the same instance — and IdempotencyKey is what the far end is given so it
// can recognise the repeat too. A second visit, through a loop, is a new job and
// a new call.
//
// It cannot share the transaction that advances the token: the call leaves the
// process, and a transaction that rolled back after it would have unmade the
// record of something the outside world already saw.
type ServiceCall struct {
	storm.Model

	Instance ProcessInstance
	Project  Project
	// Job is the visit; null on rows written before it was part of the identity.
	Job *Job

	NodeID      string
	IterationID string

	IdempotencyKey string
	Status         string
	Attempts       int
	Response       storm.JSON
	CompletedAt    *time.Time

	DeletedAt *time.Time
}

func (s *ServiceCall) Schema(t *storm.Table) {
	t.Col(&s.NodeID).Size(191)
	t.Col(&s.IterationID).Size(191)
	// One record per visit to a step of an instance: this is what makes a
	// retry find the call rather than make a second one.
	// Across the deleted rows: this key IS the de-duplication. Scoped to the
	// live ones, a marked row stops conflicting, the retry inserts a second
	// record and makes the outbound call again — which is the one thing this
	// table exists to prevent.
	t.UniqueAcrossDeleted(&s.Instance, &s.NodeID, &s.IterationID, &s.Job)
	t.Col(&s.IdempotencyKey).Size(128)
	t.Col(&s.IdempotencyKey).Index()
	t.Col(&s.Status).Size(32)
	t.Col(&s.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&s.DeletedAt)
}
