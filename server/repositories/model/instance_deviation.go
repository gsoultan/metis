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

	RequestID    *storm.UUID
	ApprovedBy   *string
	ApprovedByID *storm.UUID
	DecidedAt    *time.Time
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
	t.Col(&d.ApprovedBy).Size(255)
	t.Index(&d.InstanceID, &d.CreatedAt, &d.ID).Named("ix_instance_deviations_instance")
	t.Index(&d.RunID).Named("ix_instance_deviations_run")
	t.Index(&d.InstanceID, &d.VisitKey).Unique().Named("ux_instance_deviations_visit")
}
