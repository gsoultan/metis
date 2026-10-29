package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// SharedCounter is one replica's share of a rate-limit window.
//
// Each replica keeps its own row and the limit is the sum, so counting does not
// need a lock and a replica that dies stops contributing rather than holding
// the window open. The four-part key is the identity: a scope, what is being
// counted, who counted it, and which window.
//
// The column is counter_key rather than key, which is reserved on several
// engines.
type SharedCounter struct {
	Scope       string
	Key         string
	Replica     string
	WindowStart time.Time

	Count     int64
	UpdatedAt time.Time
}

func (s *SharedCounter) Schema(t *storm.Table) {
	// The rename comes first: PrimaryKey resolves field pointers to column
	// names as it is called, so declaring the key before renaming the column
	// builds a constraint over a column that does not exist. PostgreSQL says so
	// at apply time, which is a long way from here.
	t.Col(&s.Key).Named("counter_key").Size(255)
	t.Col(&s.Scope).Size(64)
	t.Col(&s.Replica).Size(64)
	t.PrimaryKey(&s.Scope, &s.Key, &s.Replica, &s.WindowStart)
	t.Col(&s.UpdatedAt).Index()
	// The closed windows the prune removes.
	t.Index(&s.WindowStart).Named("ix_shared_counters_window_start")
	// Closed windows are pruned a batch at a time (DeleteBatch).
	t.Takes()
}
