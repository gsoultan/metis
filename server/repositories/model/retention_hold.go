package model

import "github.com/gsoultan/storm"

// RetentionHold keeps one instance, and the whole case of calls it is part
// of, out of the finished-instance sweep for as long as it stands:
// a legal hold, an investigation, an auditor who asked for the case to be
// kept.
//
// A row is the hold itself. Releasing one deletes it, and the audit trail —
// which no sweep touches — keeps who placed it, who released it and why.
type RetentionHold struct {
	storm.Model

	Project Project
	// InstanceID is a plain column, as instance_deviations.instance_id is: a
	// foreign key to a row a completion holds would make placing a hold wait
	// on the engine, and the engine never needs to know of one.
	InstanceID storm.UUID

	Reason     string
	PlacedBy   string
	PlacedByID storm.UUID
}

func (h *RetentionHold) Schema(t *storm.Table) {
	t.Name("retention_holds")
	t.Col(&h.PlacedBy).Size(255)
	// One hold per instance. The sweep reads every hold once per batch, and
	// the instance's own page reads its hold by instance.
	t.Index(&h.InstanceID).Unique().Named("ux_retention_holds_instance")
	// A project's holds, newest first.
	t.Index(&h.Project, &h.CreatedAt, &h.ID).Named("ix_retention_holds_project")
}
