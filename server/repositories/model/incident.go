package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// Incident is a failure somebody has to look at: a job that exhausted its
// retries, a gateway whose conditions were all refused.
//
// Raised rather than logged, because a process that cannot advance is not an
// error in a stream somebody may read — it is work that has stopped.
type Incident struct {
	storm.Model

	Instance ProcessInstance

	// JobID and DefinitionID are recorded rather than referenced: an incident
	// outlives the job it came from, and reading it is how somebody finds out
	// what failed after the job row is gone.
	Job        *Job
	Definition *ProcessDefinition

	NodeID string
	Error  string
	Status IncidentStatus

	ResolvedAt *time.Time

	DeletedAt *time.Time
}

func (i *Incident) Schema(t *storm.Table) {
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&i.NodeID).FillNulls(blank)
	t.Col(&i.Error).FillNulls(blank)
	t.Col(&i.Status).FillNulls(blank)
	// No size: a step's id is the definition's, and nothing at deploy limits how long it is.
	t.Col(&i.Status).Size(32)
	t.Col(&i.Status).Index()
	// An incident outlives the job it came from: the job row is cleaned up,
	// the account of why it failed is not.
	t.Col(&i.Job).OnDelete(storm.SetNull).Index()
	t.Col(&i.Definition).OnDelete(storm.SetNull).Index()
	t.Col(&i.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&i.DeletedAt)
	// The instance list asks which instances on a page hold an open incident,
	// through the instance so its scope and soft delete apply.
	t.Probes(&i.Instance)
}

// Aggregates declares ByInstance: how many incidents each instance holds,
// for the instance list's attention marker.
func (i *Incident) Aggregates(a *storm.Aggregates) {
	byInstance := a.Named("ByInstance")
	byInstance.By(&i.Instance)
	byInstance.Count("Total")
	// Any incident, removed or not: the finished-instance purge keeps an
	// instance while one of its incidents is still open, marked or not.
	anyByInstance := a.Named("AnyByInstance").AcrossDeleted()
	anyByInstance.By(&i.Instance)
	anyByInstance.Count("Total")
}
