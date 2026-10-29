package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// AuditEntry is one line of the business timeline: what happened, to which
// node, and in words a person can read.
//
// Narrative is stored rather than generated at read time so the account of what
// happened cannot change when the code that describes it does.
type AuditEntry struct {
	storm.Model

	Project  Project
	Instance ProcessInstance

	Type     string
	NodeID   *string
	NodeName *string
	Message  string
	// Narrative is the plain-English account shown on the timeline.
	Narrative *string
	// Data is the structured detail behind the line, as jsonb.
	Data storm.JSON

	// Seq is the entry's place in the order entries were written, drawn from a
	// sequence by the column's default, so no writer supplies it. The trail is
	// read by created_at and then by this: created_at is the moment the
	// writing transaction began, which every entry one step writes shares.
	//
	// Entries written before GORM's migration 28 had no place, because
	// nothing recorded one, and were NULL. They are 0, which keeps them where
	// the trail read them: ahead of every numbered entry of their moment.
	Seq int64

	// Actor is the person who acted, for an entry somebody's act wrote: a
	// waive, a migration, a reassignment, a hold. Nil for the engine's own
	// entries, and for every entry written before migration 46, whose actor
	// is only in Data. Plain, unlike Data, so the audit log can be filtered by
	// it.
	Actor *string

	DeletedAt *time.Time
}

func (a *AuditEntry) Schema(t *storm.Table) {
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&a.Type).FillNulls(blank)
	t.Col(&a.Message).FillNulls(blank)
	t.Col(&a.Data).ConvertFrom(storm.JSONText).FillNulls(jsonNull)
	t.Name("audit_logs")
	t.Col(&a.Type).Size(64)
	// No size: a step's id is the definition's, and nothing at deploy limits how long it is.
	t.Col(&a.Actor).Size(255)
	t.Col(&a.Seq).Serial().FillNulls("0")
	t.Col(&a.DeletedAt).Index()
	// The trail outlives the instance it is the record of: the finished-
	// instance sweep removes an instance and everything that existed only to
	// run it, and keeps this. A key would refuse the sweep, or take the trail
	// with it.
	t.Col(&a.Instance).AcknowledgeNoFK("the audit trail outlives the instance it records")
	// The audit log: the entries that name somebody, newest first, and one
	// person's. Partial, so they are the size of what people did rather than
	// of everything the engine wrote. Spelled as PostgreSQL prints it back.
	t.Index(&a.CreatedAt, &a.Seq).Where("(actor IS NOT NULL)").Named("ix_audit_logs_operations")
	t.Index(&a.Actor, &a.CreatedAt, &a.Seq).Where("(actor IS NOT NULL)").Named("ix_audit_logs_operations_by_actor")
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&a.DeletedAt)
}

// Aggregates declares Visits: how often an instance's trail reached each node,
// and where it first did — the moment, and the seq of the first entry in the
// trail's order, created_at and then seq with the unnumbered entries first.
func (a *AuditEntry) Aggregates(g *storm.Aggregates) {
	visits := g.Named("Visits")
	visits.By(&a.NodeID)
	visits.Count("Visits")
	visits.Min(&a.CreatedAt, "FirstAt")
	visits.FirstOf(&a.Seq, "FirstSeq", g.Over().OrderByAsc(&a.CreatedAt).OrderByAsc(&a.Seq))
}
