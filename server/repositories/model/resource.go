package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// Resource is one file inside a deployment — a BPMN document, a decision table,
// a form.
type Resource struct {
	storm.Model

	Deployment Deployment

	Name string
	// Content is the file as bytes, whatever it is.
	Content []byte
	Type    string

	DeletedAt *time.Time
}

func (r *Resource) Schema(t *storm.Table) {
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&r.Name).FillNulls(blank)
	t.Col(&r.Type).FillNulls(blank)
	// deployment_resources, not resources. The table exists and holds data;
	// storm would derive the generic name from the type, and a table with two
	// names is the drift this port exists to end — but renaming a live table for
	// tidiness is not worth the lock, and the longer name says more.
	t.Name("deployment_resources")
	t.Col(&r.Name).Size(255)
	t.Col(&r.Type).Size(64)
	t.Col(&r.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&r.DeletedAt)
}
