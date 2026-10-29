package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// Organization is the tenant. Every scoped read in the system ultimately asks
// whether a row belongs to one of these.
type Organization struct {
	storm.Model

	Name        string
	Description *string

	// Projects is the has-many. Reading it requires a declared plan, so an
	// N+1 is not something this can do by accident.
	Projects []Project

	DeletedAt *time.Time
}

func (o *Organization) Schema(t *storm.Table) {
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&o.Name).FillNulls(blank)
	t.Col(&o.Name).Size(255)
	t.Col(&o.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&o.DeletedAt)
}
