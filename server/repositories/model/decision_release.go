package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// DecisionRelease is one entry in a decision key's release timeline: from
// ActivateAt onwards, an evaluation that names no version reads Version.
//
// The same shape and the same rule as ProcessDefinitionRelease — the live
// version is the newest entry whose moment has come — so the two kinds of
// model answer "which version is live" the same way. Saving a decision only
// adds a version; which one is live is this table's, and making an older one
// live again is one more entry, not a rewrite of what went before.
//
// An evaluation pinned to a version ignores it.
type DecisionRelease struct {
	storm.Model

	Project Project

	// DecisionKey, not Key, for the reason ProcessKey is: it leads the unique
	// index, and the hot lookup — which version of this key is live — runs on
	// every business rule task that names no version.
	DecisionKey string
	// ActivateAt is when this entry took over, in UTC.
	ActivateAt time.Time
	Version    int

	DeletedAt *time.Time
}

func (r *DecisionRelease) Schema(t *storm.Table) {
	// As long as the key it names can be: decision keys are 255 characters.
	t.Col(&r.DecisionKey).Size(255)
	// Two versions cannot claim the same instant, and making a version live
	// again is a new entry rather than an overwrite of the history.
	t.Unique(&r.DecisionKey, &r.Project, &r.ActivateAt)
	t.Col(&r.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&r.DeletedAt)
}
