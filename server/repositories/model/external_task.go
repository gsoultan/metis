package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// ExternalTask is work an outside worker fetches and completes, rather than
// something the engine executes itself.
//
// The worker holds it under a lock with an expiry, so a worker that dies
// releases its work rather than stranding it.
type ExternalTask struct {
	storm.Model

	Project    Project
	Instance   ProcessInstance
	Definition ProcessDefinition

	NodeID string
	// Topic is what a worker subscribes to.
	Topic          string
	WorkerID       *string
	LockExpiration *time.Time

	Retries      int
	RetryTimeout int64
	ErrorMessage *string
	ErrorDetails *string

	Variables storm.JSON

	DeletedAt *time.Time
}

func (e *ExternalTask) Schema(t *storm.Table) {
	// GORM's defaults, kept. See fill.go.
	t.Col(&e.Retries).Default(zero)
	t.Col(&e.RetryTimeout).Default(zero)
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&e.NodeID).FillNulls(blank)
	t.Col(&e.Topic).FillNulls(blank)
	t.Col(&e.Variables).ConvertFrom(storm.JSONText).FillNulls(jsonNull)
	t.Col(&e.NodeID).Size(255)
	t.Col(&e.Topic).Size(255)
	t.Col(&e.Topic).Index()
	t.Col(&e.WorkerID).Size(255)
	t.Col(&e.WorkerID).Index()
	t.Col(&e.LockExpiration).Index()
	t.Col(&e.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&e.DeletedAt)
	// An incident at a task's step: the same instance and the same node,
	// which no foreign key between the two says.
	var i Incident
	t.ProbesOn("StepIncidents", &i, &e.Instance, &i.Instance, &e.NodeID, &i.NodeID)
}

// Aggregates declares the topic list's counts: the tasks on offer by topic,
// and the tasks under a live lock by topic and the worker holding them.
func (e *ExternalTask) Aggregates(a *storm.Aggregates) {
	waiting := a.Named("WaitingByTopic")
	waiting.By(&e.Topic)
	waiting.Count("Total")
	held := a.Named("HeldByWorker")
	held.By(&e.Topic, &e.WorkerID)
	held.Count("Total")
	// The topics on offer in each project, which the unserved count reads
	// across every tenant.
	byProject := a.Named("WaitingByProject")
	byProject.By(&e.Project, &e.Topic)
	byProject.Count("Total")
}
