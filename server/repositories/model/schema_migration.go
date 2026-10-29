package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// SchemaMigration records one applied migration: the answer to which version
// a database is at.
//
// The table predates the model. GORM created it, with every column but the
// version nullable, so the fields that are nullable there are pointers here.
// Version is an identity the migration list assigns and never a sequence: a
// database that renumbered it would lose track of which migration is which.
type SchemaMigration struct {
	Version    int64
	Name       *string
	AppliedAt  *time.Time
	DurationMS *int64
}

func (m *SchemaMigration) Schema(t *storm.Table) {
	t.Name("schema_migrations")
	t.Col(&m.Name).Size(255)
	t.Col(&m.DurationMS).Named("duration_ms")
	t.PrimaryKey(&m.Version)
	// Locked by the replica applying migrations, so another one waits and
	// then reads what the first applied rather than applying it again.
	t.Lockable()
}
