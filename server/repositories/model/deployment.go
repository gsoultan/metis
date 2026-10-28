package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// Deployment is a set of resources deployed together.
type Deployment struct {
	storm.Model

	Project Project
	Name    string

	Resources []Resource

	DeletedAt *time.Time
}

func (d *Deployment) Schema(t *storm.Table) {
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&d.Name).FillNulls(blank)
	t.Col(&d.Name).Size(255)
	t.Col(&d.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&d.DeletedAt)
}
