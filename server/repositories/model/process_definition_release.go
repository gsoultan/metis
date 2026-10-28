package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// ProcessDefinitionRelease is one entry in a process key's release timeline:
// from ActivateAt onwards, new instances start on Version.
//
// A timeline rather than a single current-version row, because that is what
// makes a cutover something you can arrange in advance. The live version is a
// pure function of the rows and the clock — the newest whose time has come — so
// nothing has to wake up to apply it and no replica can miss it.
//
// It says nothing about instances already running: those pin their definition
// by id and finish on the version they started on.
type ProcessDefinitionRelease struct {
	storm.Model

	Project Project

	// ProcessKey, not Key, and it leads the unique index: the hot lookup is
	// "which version of this key is live", which runs on every process start.
	ProcessKey string
	// ActivateAt is when this entry takes over, in UTC. Past means it already
	// has; future is a cutover somebody arranged.
	ActivateAt time.Time
	Version    int

	DeletedAt *time.Time
}

func (r *ProcessDefinitionRelease) Schema(t *storm.Table) {
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&r.ProcessKey).FillNulls(blank)
	t.Col(&r.ProcessKey).Size(191)
	// The triple is unique so two versions cannot claim the same instant, and
	// promoting again is a new entry rather than an overwrite of the history.
	// Live rows only, deliberately: cancelling a scheduled cutover marks its
	// row, and arranging one again for the same instant is a thing people do.
	t.Unique(&r.ProcessKey, &r.Project, &r.ActivateAt)
	t.Col(&r.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&r.DeletedAt)
}
