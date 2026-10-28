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
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&d.Key).FillNulls(blank)
	t.Col(&d.Name).FillNulls(blank)
	t.Col(&d.HitPolicy).FillNulls(blank)
	t.Col(&d.RequiredDecisions).ConvertFrom(storm.JSONText).FillNulls(jsonNull)
	t.Col(&d.Inputs).ConvertFrom(storm.JSONText).FillNulls(jsonNull)
	t.Col(&d.Outputs).ConvertFrom(storm.JSONText).FillNulls(jsonNull)
	t.Col(&d.Rules).ConvertFrom(storm.JSONText).FillNulls(jsonNull)
	t.Col(&d.Tests).ConvertFrom(storm.JSONText).FillNulls(jsonNull)
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
// reissued. Keys is each key's newest version and when it was last saved,
// over the versions that are not deleted.
func (d *DecisionDefinition) Aggregates(a *storm.Aggregates) {
	a.Named("HighestVersion").AcrossDeleted().Max(&d.Version, "Version")
	keys := a.Named("Keys")
	keys.By(&d.Project, &d.Key)
	keys.Max(&d.Version, "Newest")
	keys.Max(&d.CreatedAt, "SavedAt")
}

// Joins declares Shown: one row per key, the version a release made live —
// the newest release whose moment has come, when that version is not deleted
// — or else the newest. The caller narrows the keys and the releases.
func (d *DecisionDefinition) Joins(j *storm.Joins) {
	var r DecisionRelease
	var live DecisionDefinition
	j.Named("Shown").
		WithWhere("keys", d, "Keys").
		WithWhere("released", &r, "Live").
		InnerWith("keys", j.OnCols("keys", "project_id", &d.Project).And("keys", "key", &d.Key)).
		LeftWith("released", j.OnCols("released", "project_id", &d.Project).And("released", "decision_key", &d.Key)).
		LeftAs("live", &live, j.OnCols("decision_definitions", "project_id", &live.Project).
			And("decision_definitions", "key", &live.Key).And("released", "version", &live.Version)).
		Where(j.Eq(&d.Version, j.Coalesce(&live.Version, j.ColOf("keys", "newest")))).
		Take(&d.ID, "ID").
		Take(&d.Project, "ProjectID").
		Take(&d.Key, "Key").
		Take(&d.Name, "Name").
		Take(&d.Version, "Version").
		Take(&d.RequiredDecisions, "RequiredDecisions").
		Take(&d.HitPolicy, "HitPolicy").
		Take(&live.Version, "LiveVersion").
		TakeFrom("keys", "newest", "Newest").
		TakeFrom("keys", "saved_at", "SavedAt").
		TakeFrom("released", "activate_at", "ActivatedAt").
		OrderAsc(&d.Key).
		OrderAsc(&d.Project)
}
