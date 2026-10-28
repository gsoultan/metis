package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// Project groups a set of process models and the environments they run in.
//
// It is also what tenant scoping resolves through: a row belongs to an
// organization because the project it names does.
type Project struct {
	storm.Model

	// Organization is the foreign key. Declared as the type rather than an id
	// so the relation is checked by the compiler and follows a rename.
	Organization Organization

	Name        string
	Description *string

	DeletedAt *time.Time
}

func (p *Project) Schema(t *storm.Table) {
	t.Col(&p.Name).Size(255)
	// A project's name is unique inside its organization, not globally: two
	// tenants both having a "Payments" project is normal.
	// Live rows only: a deleted project's name is available again.
	t.Unique(&p.Organization, &p.Name)
	t.Col(&p.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&p.DeletedAt)
	// The unserved-topic count names the organization of every project with
	// a task on offer, removed or not.
	t.ReadsDeleted()
}

// Projections declares the tenant scope's read: an organization's projects by
// id and nothing else. Every scoped request reads it, and an organization can
// have thousands of projects, so it carries the 16 bytes it is for rather than
// each whole row.
func (p *Project) Projections(pr *storm.Projections) {
	pr.Named("Key", &p.ID)
}
