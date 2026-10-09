package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// DeviationRequest is a deviation waiting for a second administrator (D9):
// an in-place waive, or a migration that loosens a rule on work still
// running (a skip, a control taken, a redirect past one, a loosened
// separation-of-duties rule).
// A compliance record: no deleted_at, no retention sweep.
type DeviationRequest struct {
	storm.Model

	Project Project
	Kind    string
	Status  string
	// InstanceID is a plain column, as instance_deviations.instance_id is: a
	// foreign key to a row a completion holds would deadlock a racing request.
	InstanceID       *storm.UUID
	SourceDefinition *ProcessDefinition
	TargetDefinition *ProcessDefinition

	RequestedBy   string
	RequestedByID storm.UUID
	Reason        string

	// Command and Plan are sealed by the repository; they hold business data.
	Command storm.JSON
	Plan    storm.JSON

	Fingerprint string
	// LiveKey is Fingerprint while the request is pending or approved, NULL
	// once decided, so one live request per visit or per migration.
	LiveKey *string
	// ApprovedInstances and Outcome are never NULL: "[]" and "{}" say there is
	// nothing in them, as the ledger's before and after do.
	ApprovedInstances storm.JSON
	ExpiresAt         time.Time

	DecidedBy      *string
	DecidedByID    *storm.UUID
	DecisionReason *string
	DecidedAt      *time.Time
	Outcome        storm.JSON
}

func (r *DeviationRequest) Schema(t *storm.Table) {
	t.Name("deviation_requests")
	t.Col(&r.SourceDefinition).OnDelete(storm.SetNull)
	t.Col(&r.TargetDefinition).OnDelete(storm.SetNull)
	t.Col(&r.Kind).Size(32)
	t.Col(&r.Status).Size(32)
	t.Col(&r.RequestedBy).Size(255)
	t.Col(&r.Fingerprint).Size(64)
	t.Col(&r.LiveKey).Size(64)
	t.Col(&r.DecidedBy).Size(255)
	t.Index(&r.Project, &r.Status, &r.CreatedAt, &r.ID).Named("ix_deviation_requests_queue")
	// The sweep's two reads: a status, then deadline and id — the order they
	// read in and the cursor they go on from (pg sweep), so a pass that reads
	// on from a cursor walks the index from there.
	t.Index(&r.Status, &r.ExpiresAt, &r.ID).Named("ix_deviation_requests_sweep")
	// No index on instance_id: no query reads requests by instance. A waive's
	// request is reached from its ledger row (instance_deviations.request_id),
	// and the ledger is what is read by instance.
	t.Index(&r.Project, &r.LiveKey).Unique().Named("ux_deviation_requests_live_key")
}

// Projections declares the queue's read: a request without its command, its
// plan and the instances it covers.
//
// Those three are the heavy part of the row — the instance list has no bound
// (every running instance of a version), and the other two are sealed — and
// the queue shows none of them. Read whole, a page would cost what its largest
// requests weigh, on demand, for anyone who may list. They are read one
// request at a time instead.
func (r *DeviationRequest) Projections(p *storm.Projections) {
	p.Named("Queue",
		&r.ID, &r.CreatedAt, &r.UpdatedAt, &r.Project, &r.Kind, &r.Status,
		&r.InstanceID, &r.SourceDefinition, &r.TargetDefinition,
		&r.RequestedBy, &r.RequestedByID, &r.Reason, &r.Fingerprint, &r.ExpiresAt,
		&r.DecidedBy, &r.DecidedByID, &r.DecisionReason, &r.DecidedAt, &r.Outcome)
}
