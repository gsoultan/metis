package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// ArchivedInstance is a finished instance that has been moved out of the
// tables that run processes, kept whole and read-only.
//
// The retention sweep writes one when a completed or cancelled instance has
// been finished for longer than its period: the instance, its tasks, service
// calls, incidents, notifications, variable history and audit trail, as they
// last stood, in one sealed record. The live rows then go, so the tables the
// engine works in stay the size of the work in flight, and the record can
// still be opened from the project's archive.
//
// The id is the instance's own, so a link to the instance finds its archive,
// and an instance is archived once. CreatedAt is when it was archived.
//
// The columns beside Record are what the archive is searched by. Nothing in
// them is personal data: the business key is what an operator searches live
// instances by too (see ProcessInstance.BusinessKey).
type ArchivedInstance struct {
	storm.Model

	Project Project
	// Definition is the version the instance ran on. It has no key: the
	// archive outlives the version, which can be deleted once nothing live
	// runs on it.
	Definition ProcessDefinition

	// What the version was called, kept here because the version may not be.
	ProcessKey     string
	ProcessName    string
	ProcessVersion int32

	BusinessKey *string
	Status      string
	StartedAt   time.Time
	FinishedAt  time.Time

	// RecordStore is where the record is kept: "database", in Record
	// itself, or a store outside the database the operator chose, where
	// Record names the object.
	RecordStore string
	// Record is everything kept of the instance — JSON, compressed, then
	// sealed with ENCRYPTION_KEY as a whole, as text, so `metis --reseal`
	// finds it like any other sealed column — or, for a record kept outside
	// the database, the name of the object that holds it, sealed the same way.
	Record string
}

func (a *ArchivedInstance) Schema(t *storm.Table) {
	t.Name("archived_instances")
	t.Col(&a.Definition).NoIndex().AcknowledgeNoFK("the archive outlives the version the instance ran on")
	t.Col(&a.ProcessKey).Size(255)
	t.Col(&a.ProcessName).Size(255)
	t.Col(&a.BusinessKey).Size(255)
	t.Col(&a.Status).Size(32)
	t.Col(&a.RecordStore).Size(32)
	// A project's archive, most recently finished first, and the same for one
	// process; the keyset cursor pages on finished_at and then id.
	t.Index(&a.Project, storm.Desc(&a.FinishedAt), storm.Desc(&a.ID)).Named("ix_archived_instances_project_finished")
	t.Index(&a.Project, &a.ProcessKey, storm.Desc(&a.FinishedAt), storm.Desc(&a.ID)).Named("ix_archived_instances_process_finished")
	// A project's archived instances of one case.
	t.Index(&a.Project, &a.BusinessKey).Named("ix_archived_instances_business_key")
	// The archive sweep removes records archived before its period, a batch
	// at a time.
	t.Col(&a.CreatedAt).Index()
	// The records still in their rows, when the operator has chosen a store
	// outside the database for them: the sweep moves these, oldest first.
	t.Index(&a.CreatedAt, &a.ID).Where("((record_store)::text = 'database'::text)").Named("ix_archived_instances_in_database")
	t.Takes()
}

// Projections declares Summary: every column but the record, for the list,
// which never opens one.
func (a *ArchivedInstance) Projections(p *storm.Projections) {
	p.Named("Summary", &a.ID, &a.CreatedAt, &a.Project, &a.Definition, &a.ProcessKey, &a.ProcessName,
		&a.ProcessVersion, &a.BusinessKey, &a.Status, &a.StartedAt, &a.FinishedAt)
}
