package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// CompensatableActivity is a completed step that has a compensation handler,
// recorded so it can be undone if the process later fails.
//
// Compensated is the idempotence guard: a retried or re-thrown compensation
// must not undo the same activity twice.
type CompensatableActivity struct {
	storm.Model

	Instance ProcessInstance

	NodeID             string
	CompensationNodeID string
	// Variables are the values the activity ran with, kept because the
	// compensation needs them and the instance's current ones have moved on.
	Variables   storm.JSON
	CompletedAt time.Time
	Compensated bool

	DeletedAt *time.Time
}

func (c *CompensatableActivity) Schema(t *storm.Table) {
	// GORM's defaults, kept. See fill.go.
	t.Col(&c.Compensated).Default(no)
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&c.NodeID).FillNulls(blank)
	t.Col(&c.CompensationNodeID).FillNulls(blank)
	t.Col(&c.Variables).ConvertFrom(storm.JSONText).FillNulls(jsonNull)
	t.Col(&c.Compensated).FillNulls(no)
	// No size: a step's id is the definition's, and nothing at deploy limits how long it is.
	t.Col(&c.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&c.DeletedAt)
}
