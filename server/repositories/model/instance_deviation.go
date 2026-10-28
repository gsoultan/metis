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
	// Instance and Task have no foreign key: see the lock order note in
	// tests/migrations/instance_deviations_test.go and task.go.
	Instance   ProcessInstance
	Definition *ProcessDefinition

	Kind   string
	Scope  string
	Origin string
	Status string

	NodeID      *string
	NodeName    *string
	Task        *Task
	IterationID *string

	Actor string
	// ActorUser is the account that acted, kept as actor_id.
	ActorUser *User
	Reason    *string

	// Before and After are sealed by the repository; Details is not business data.
	Before  storm.JSON
	After   storm.JSON
	Details storm.JSON

	// RunID groups the rows one act wrote. It names no row of its own.
	RunID      storm.UUID
	AuditEntry *AuditEntry
	VisitKey   *string

	// Request is the request this row was asked for or approved under.
	Request    *DeviationRequest
	ApprovedBy *string
	// ApprovedByUser is the account that approved it, kept as approved_by_id.
	ApprovedByUser *User
	DecidedAt      *time.Time

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
	// None of these has a foreign key. The ledger outlives every row it names,
	// and writing it must not wait on a row the engine holds.
	t.Col(&d.Instance).NoIndex().AcknowledgeNoFK("the ledger must not wait on an instance row a completion holds")
	t.Col(&d.Task).NoIndex().AcknowledgeNoFK("the ledger must not wait on a task row the engine holds")
	t.Col(&d.ActorUser).Named("actor_id").NoIndex().AcknowledgeNoFK("the ledger outlives the account that acted")
	t.Col(&d.AuditEntry).NoIndex().AcknowledgeNoFK("the ledger outlives the audit trail's sweep")
	t.Col(&d.ApprovedByUser).Named("approved_by_id").NoIndex().AcknowledgeNoFK("the ledger outlives the account that approved")
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
	t.Index(&d.Instance, &d.CreatedAt, &d.ID).Named("ix_instance_deviations_instance")
	t.Index(&d.RunID).Named("ix_instance_deviations_run")
	t.Index(&d.Instance, &d.LiveVisitKey).Unique().Named("ux_instance_deviations_live_visit")
	t.Index(&d.Instance, &d.VisitKey).Named("ix_instance_deviations_visit")
}

// Aggregates declares ByInstance: how many ledger rows each instance has. The
// finished-instance purge keeps every instance with one.
func (d *InstanceDeviation) Aggregates(a *storm.Aggregates) {
	byInstance := a.Named("ByInstance")
	byInstance.By(&d.Instance)
	byInstance.Count("Total")
}
