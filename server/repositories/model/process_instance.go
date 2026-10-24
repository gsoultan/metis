package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// ProcessInstance is one running execution of a process definition.
//
// Definition is a hard reference to the exact version it started on, and it
// never changes: an instance finishes on the graph it began with, whatever is
// deployed afterwards. That is the guarantee the whole versioning story rests
// on, and it is why deleting a definition an instance references is refused.
type ProcessInstance struct {
	storm.Model

	Project    Project
	Definition ProcessDefinition

	// A sub-process instance knows the instance and node that called it.
	ParentInstance *ProcessInstance
	ParentNodeID   *string

	Status ProcessStatus

	// BusinessKey is what the caller who started the instance calls the case:
	// an order number, a claim reference. Optional, not unique — two processes
	// can work on one order — and a sub-process started by a call activity
	// carries its caller's. Not encrypted, unlike Variables: it is what an
	// operator searches by, so it should not be personal data.
	BusinessKey *string

	// Variables is the business data, encrypted by the repository:
	// it holds whatever the process was started with, which is routinely
	// personal or commercial.
	// A string, not storm.JSON: process variables are encrypted at rest by
	// models.EncryptedMap, so the column holds ciphertext. Declaring it jsonb
	// would fail to parse and, on the day the column types are reconciled,
	// destroy every running instance's data.
	Variables string

	// Tokens are the live points of execution — see the Token payload struct.
	Tokens storm.JSON
	// CompletedNodes and CompensatedNodes are jsonb arrays of node id.
	CompletedNodes   storm.JSON
	CompensatedNodes storm.JSON

	// MultiInstance and Joins are engine bookkeeping, kept out of Variables so
	// they cannot collide with business data or reach the audit trail through
	// it.
	MultiInstance storm.JSON
	Joins         storm.JSON

	DeletedAt *time.Time
}

func (i *ProcessInstance) Schema(t *storm.Table) {
	t.Col(&i.Status).Size(32)
	t.Col(&i.Status).Index()
	// Self-referential: a sub-process instance names the one that called it.
	// Set to null rather than cascading, so removing a parent does not delete
	// the record of work its children actually did.
	t.Col(&i.ParentInstance).OnDelete(storm.SetNull).Index()
	t.Col(&i.DeletedAt).Index()
	t.Col(&i.BusinessKey).Size(255)
	// A project's instances of one case, which the instance list searches by.
	t.Index(&i.Project, &i.BusinessKey).Named("ix_process_instances_business_key")
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&i.DeletedAt)
}

// Aggregates are the counts the instance list and the version history show.
// The grouping is fixed here; the scope and filters are still the caller's
// predicates.
func (i *ProcessInstance) Aggregates(a *storm.Aggregates) {
	// How many instances are in each state: one filter chip per state.
	byStatus := a.Named("ByStatus")
	byStatus.By(&i.Status)
	byStatus.Count("Total")

	// How much work each version carries, split by state.
	byDefinition := a.Named("ByDefinitionAndStatus")
	byDefinition.By(&i.Definition, &i.Status)
	byDefinition.Count("Total")
}
