package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// InstanceDeviation is one row of an instance's ledger: an act on the instance
// that its process did not decide.
//
// An append-mostly compliance record. It has no deleted_at and no retention
// sweep: a row leaves only when its project does.
type InstanceDeviation struct {
	storm.Model

	Project Project
	// InstanceID and TaskID are plain columns, not references: see the lock
	// order note in tests/migrations/instance_deviations_test.go and task.go.
	InstanceID storm.UUID
	Definition *ProcessDefinition

	Kind   string
	Scope  string
	Origin string
	Status string

	NodeID      *string
	NodeName    *string
	TaskID      *storm.UUID
	IterationID *string

	Actor   string
	ActorID *storm.UUID
	Reason  *string

	// Before and After are sealed by the repository; Details is not business data.
	Before  storm.JSON
	After   storm.JSON
	Details storm.JSON

	RunID        storm.UUID
	AuditEntryID *storm.UUID
	VisitKey     *string

	// Request is the request this row was asked for or approved under.
	Request      *DeviationRequest
	ApprovedBy   *string
	ApprovedByID *storm.UUID
	DecidedAt    *time.Time

	// LiveVisitKey is VisitKey while the row is applied or awaiting approval,
	// NULL once it is rejected, expired or stale: one live row per visit, and a
	// decided one no longer refuses a new request for it. A column rather than
	// a partial unique index on status, which the drift check cannot compare.
	//
	// Last, where migration 34 adds it to a ledger that already exists, so a
	// table built from the model and an upgraded one have their columns in the
	// same order.
	LiveVisitKey *string
}

func (d *InstanceDeviation) Schema(t *storm.Table) {
	t.Name("instance_deviations")
	t.Col(&d.Definition).OnDelete(storm.SetNull)
	t.Col(&d.Kind).Size(32)
	t.Col(&d.Scope).Size(16)
	t.Col(&d.Origin).Size(16)
	t.Col(&d.Status).Size(32)
	t.Col(&d.NodeID).Size(255)
	t.Col(&d.NodeName).Size(255)
	t.Col(&d.IterationID).Size(191)
	t.Col(&d.Actor).Size(255)
	t.Col(&d.VisitKey).Size(64)
	t.Col(&d.LiveVisitKey).Size(64)
	t.Col(&d.ApprovedBy).Size(255)
	t.Index(&d.InstanceID, &d.CreatedAt, &d.ID).Named("ix_instance_deviations_instance")
	t.Index(&d.RunID).Named("ix_instance_deviations_run")
	t.Index(&d.InstanceID, &d.LiveVisitKey).Unique().Named("ux_instance_deviations_live_visit")
	t.Index(&d.InstanceID, &d.VisitKey).Named("ix_instance_deviations_visit")
}
