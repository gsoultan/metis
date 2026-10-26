package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// ProcessDefinition is one deployed version of a process model.
//
// A version is immutable once deployed, and (project, key, version) is unique:
// versions are allocated by reading the highest and adding one, which two
// concurrent deploys do from the same number. Without the constraint both
// writes succeed, two rows claim to be version N, and which one "start version
// N" gets is a coin flip.
type ProcessDefinition struct {
	storm.Model

	Project Project

	Key     string
	Name    string
	Version int

	// Nodes and Flows are the graph, as jsonb. They are the whole BPMN document
	// and are projected away by list reads, which do not need them.
	Nodes storm.JSON
	Flows storm.JSON

	// Deployment groups the resources deployed together, when there was one.
	Deployment *Deployment

	DeletedAt *time.Time
}

func (d *ProcessDefinition) Schema(t *storm.Table) {
	t.Col(&d.Key).Size(255)
	t.Col(&d.Key).Index()
	// Across the deleted rows: a version number, once used, must never be
	// reissued. Instances, audit entries and release rows all name a version by
	// number, so a deleted v3 replaced by a different v3 would silently point
	// every one of them at a graph the process never ran.
	t.UniqueAcrossDeleted(&d.Project, &d.Key, &d.Version)
	// A definition outlives the deployment that carried it: removing the
	// deployment record must not remove the process people are running.
	t.Col(&d.Deployment).OnDelete(storm.SetNull).Index()
	t.Col(&d.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&d.DeletedAt)
}

// Aggregates declares the process list's read: each key once, however many
// versions it has, in key order.
func (d *ProcessDefinition) Aggregates(a *storm.Aggregates) {
	keys := a.Named("ByKey")
	keys.By(&d.Key)
	keys.Count("Versions")
}
