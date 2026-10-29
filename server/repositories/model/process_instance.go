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

	// MultiInstance and JoinArrivals are engine bookkeeping, kept out of
	// Variables so they cannot collide with business data or reach the audit
	// trail through it. JoinArrivals is the joins column: the field cannot be
	// Joins, which is the name of the method declaring this table's joins.
	MultiInstance storm.JSON
	JoinArrivals  storm.JSON

	DeletedAt *time.Time

	// Incidents is the inverse of Incident.Instance: no column here. It is
	// what lets a read of instances ask which have an open incident, in the
	// same statement.
	Incidents []Incident
}

func (i *ProcessInstance) Schema(t *storm.Table) {
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&i.Status).FillNulls(blank)
	t.Col(&i.Variables).FillNulls(blank)
	t.Col(&i.Tokens).ConvertFrom(storm.JSONText).FillNulls(jsonNull)
	t.Col(&i.CompletedNodes).ConvertFrom(storm.JSONText).FillNulls(jsonNull)
	t.Col(&i.CompensatedNodes).ConvertFrom(storm.JSONText).FillNulls(jsonNull)
	t.Col(&i.MultiInstance).ConvertFrom(storm.JSONText).FillNulls(jsonNull)
	t.Col(&i.JoinArrivals).ConvertFrom(storm.JSONText).FillNulls(jsonNull)
	t.Col(&i.Status).Size(32)
	t.Col(&i.Status).Index()
	// Self-referential: a sub-process instance names the one that called it.
	// Set to null rather than cascading, so removing a parent does not delete
	// the record of work its children actually did.
	t.Col(&i.ParentInstance).OnDelete(storm.SetNull).Index()
	t.Col(&i.DeletedAt).Index()
	t.Col(&i.BusinessKey).Size(255)
	t.Col(&i.JoinArrivals).Named("joins")
	// A project's instances of one case, which the instance list searches by.
	t.Index(&i.Project, &i.BusinessKey).Named("ix_process_instances_business_key")
	// The instance list: newest first, in every state and in one.
	t.Index(&i.Project, storm.Desc(&i.CreatedAt)).Named("ix_process_instances_project_created")
	t.Index(&i.Project, &i.Status, storm.Desc(&i.CreatedAt)).Named("ix_process_instances_project_status")
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&i.DeletedAt)
	// The payload sweep asks which instances still have a row, removed or
	// not, before it forgets their kept values: a read of the marked rows.
	t.ReadsDeleted()
}

// Projections declares Ref, the key alone, for reads that only ask whether
// a row is there.
func (i *ProcessInstance) Projections(p *storm.Projections) {
	p.Named("Ref", &i.ID)
	// Lineage is where an instance sits in its case: the instance it was
	// called from, for the walk the retention holds take.
	p.Named("Lineage", &i.ID, &i.ParentInstance)
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

	// How many children each instance called, removed ones included: the
	// finished-instance purge keeps an instance with a child still running.
	childrenByParent := a.Named("ChildrenByParent").AcrossDeleted()
	childrenByParent.By(&i.ParentInstance)
	childrenByParent.Count("Total")

	// Where running work is sitting: the live tokens per process key and step,
	// and in the rows with no step how many instances each key has. A token is
	// live until it completes; an instance whose tokens are not an array holds
	// none. The name is the newest version's, as the dashboard shows it. The
	// call site says which instances are running and whose they are.
	var d ProcessDefinition
	waiting := a.Named("WaitingByStep").
		Join(&d, &i.Definition).
		Elements("t", &i.Tokens).
		Where(a.Ne(a.Coalesce(a.Field("t", "status"), a.Lit("")), a.Lit("completed")))
	waiting.By(&d.Key)
	node := waiting.ByExpr("Node", a.Field("t", "node_id"))
	waiting.Sets([]string{"Key", "Node"}, []string{"Key"})
	waiting.FirstOf(&d.Name, "Name", a.Over().OrderByDesc(&d.Version))
	waiting.Count("Waiting")
	waiting.CountDistinct(&i.ID, "Instances")
	waiting.GroupingOf("Total", node)
}

// Joins declares Organization: whose work an instance is, through its
// project. A job that does not name its organization is claimed in that
// organization's queue.
func (i *ProcessInstance) Joins(j *storm.Joins) {
	var p Project
	j.Named("Organization").
		Inner(&p, &i.Project).
		Take(&p.Organization, "OrganizationID")
}
