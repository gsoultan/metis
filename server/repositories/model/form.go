package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// Form is the shape of what a person fills in to complete a task.
type Form struct {
	storm.Model

	Project Project

	Key  string
	Name string
	// Fields is the form definition as jsonb. Its logic is evaluated by a
	// bounded evaluator, never by the browser's JavaScript engine: a form comes
	// from a process definition, which is authored content.
	//
	// Named Fields rather than Schema for the same reason Connector.Properties
	// was renamed — Schema is storm's declaration hook.
	Fields storm.JSON

	DeletedAt *time.Time
}

func (f *Form) Schema(t *storm.Table) {
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&f.Key).FillNulls(blank)
	t.Col(&f.Name).FillNulls(blank)
	t.Col(&f.Fields).ConvertFrom(storm.JSONText).FillNulls(jsonNull)
	t.Col(&f.Key).Size(255)
	t.Col(&f.Key).Index()
	t.Col(&f.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&f.DeletedAt)
}
