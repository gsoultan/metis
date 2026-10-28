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
	// Nullable, and plain here rather than Serial: entries written before
	// migration 28 have no place, because nothing recorded one, and a serial
	// column is NOT NULL. The sequence and the default are the migration's —
	// it makes the sequence owned by the column, which is what storm reads
	// back as a serial and leaves out of the drift report.
	Seq *int64

	// Actor is the person who acted, for an entry somebody's act wrote: a
	// waive, a migration, a reassignment, a hold. Nil for the engine's own
	// entries, and for every entry written before migration 46, whose actor
	// is only in Data. Plain, unlike Data, so the audit log can be filtered by
	// it.
	Actor *string

	DeletedAt *time.Time
}

func (a *AuditEntry) Schema(t *storm.Table) {
	t.Name("audit_logs")
	t.Col(&a.Type).Size(64)
	t.Col(&a.NodeID).Size(255)
	t.Col(&a.Actor).Size(255)
	t.Col(&a.DeletedAt).Index()
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
	visits.FirstOf(&a.Seq, "FirstSeq", g.Over().OrderByAsc(&a.CreatedAt).OrderByAscNullsFirst(&a.Seq))
}
