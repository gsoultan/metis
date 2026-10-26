package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// DecisionDefinition is one deployed version of a decision table.
//
// Versioned and immutable for the same reason a process is: an audit entry
// saying a rule fired has to be able to say which version was in force, and a
// table that changed underneath it could not.
type DecisionDefinition struct {
	storm.Model

	Project Project

	Key     string
	Name    string
	Version int

	// HitPolicy decides what happens when several rules match; Aggregation is
	// how their outputs combine when the policy collects rather than picks.
	HitPolicy   string
	Aggregation *string

	// The table itself, as jsonb: which decisions this one depends on, its
	// inputs and outputs, its rules, and the examples it is expected to get
	// right.
	RequiredDecisions storm.JSON
	Inputs            storm.JSON
	Outputs           storm.JSON
	Rules             storm.JSON
	Tests             storm.JSON

	DeletedAt *time.Time
}

func (d *DecisionDefinition) Schema(t *storm.Table) {
	t.Col(&d.Key).Size(255)
	t.Col(&d.Key).Index()
	// Across the deleted rows, for the same reason process definitions are: a
	// decision version is named by number in the instances that used it.
	t.UniqueAcrossDeleted(&d.Project, &d.Key, &d.Version)
	t.Col(&d.HitPolicy).Size(32)
	t.Col(&d.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&d.DeletedAt)
}

// Aggregates declares HighestVersion over every row, deleted ones included:
// the unique key covers them, so a version number once used is never
// reissued.
func (d *DecisionDefinition) Aggregates(a *storm.Aggregates) {
	a.Named("HighestVersion").AcrossDeleted().Max(&d.Version, "Version")
}
