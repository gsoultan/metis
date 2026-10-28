package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// VariableSnapshot is what an instance's variables were at one moment.
//
// Kept so "why did it take that branch" is answerable after the fact: the
// current variables say what is true now, not what was true when the gateway
// decided.
type VariableSnapshot struct {
	storm.Model

	Instance ProcessInstance

	NodeID     *string
	Variables  storm.JSON
	CapturedAt time.Time

	DeletedAt *time.Time
}

func (v *VariableSnapshot) Schema(t *storm.Table) {
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&v.Variables).ConvertFrom(storm.JSONText).FillNulls(jsonNull)
	t.Col(&v.NodeID).Size(255)
	t.Col(&v.CapturedAt).Index()
	t.Col(&v.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&v.DeletedAt)
	// Old snapshots are purged a batch at a time (HardDeleteBatch).
	t.Takes()
}
