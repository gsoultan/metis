package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// Task is a unit of human work an instance is waiting on.
//
// Assignee and the candidate lists are usernames, not foreign keys to User: a
// process is authored against people who may have no account, and the runtime
// moves between environments without the account list.
type Task struct {
	storm.Model

	Project  Project
	Instance ProcessInstance

	NodeID string
	// IterationID is nullable: a step that runs once has no iteration, and no
	// task created before migration 31 recorded one.
	IterationID *string
	Name        string
	Description *string
	Type        NodeType
	Status      TaskStatus

	Assignee        *string
	CandidateUsers  storm.JSON
	CandidateGroups storm.JSON

	// Owner is who delegated the task and waits for it back, a username like
	// the assignee. DelegationState is "pending" while the delegate has it and
	// "resolved" once they handed it back; both are NULL for a task never
	// delegated.
	Owner           *string
	DelegationState *string

	// Priority and DueDate are what the inbox computes urgency from. Both are
	// authored on the node and copied here when the task is created.
	Priority int
	DueDate  *time.Time

	FormKey        *string
	FormDefinition *string

	// Variables is encrypted by the repository for the same reason an
	// instance's are.
	// A string, not storm.JSON: process variables are encrypted at rest by
	// models.EncryptedMap, so the column holds ciphertext. Declaring it jsonb
	// would fail to parse and, on the day the column types are reconciled,
	// destroy every running instance's data.
	Variables string

	DeletedAt *time.Time
}

func (t2 *Task) Schema(t *storm.Table) {
	t.Col(&t2.NodeID).Size(255)
	t.Col(&t2.Type).Size(64)
	t.Col(&t2.Type).Index()
	t.Col(&t2.Status).Size(32)
	t.Col(&t2.Status).Index()
	t.Col(&t2.Assignee).Size(255)
	t.Col(&t2.Assignee).Index()
	t.Col(&t2.Owner).Size(255)
	// What somebody delegated is read by owner, for every inbox that opens.
	t.Col(&t2.Owner).Index()
	t.Col(&t2.DelegationState).Size(32)
	t.Col(&t2.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&t2.DeletedAt)
}

// Aggregates are the counts the dashboard's deadline report heads its list
// with: of the open tasks the caller's predicates choose, how many have a due
// date and how many do not. One pass over the rows, not one per count.
func (t2 *Task) Aggregates(a *storm.Aggregates) {
	deadlines := a.Named("Deadlines")
	deadlines.Count("WithDeadline").Filter(a.IsNotNull(&t2.DueDate))
	deadlines.Count("WithoutDeadline").Filter(a.IsNull(&t2.DueDate))
}

// Joins are the deadline report's rows: a task with the key and name of the
// process it is part of. Left joins, so a task whose instance or definition is
// gone still shows, with no process.
func (t2 *Task) Joins(j *storm.Joins) {
	var instance ProcessInstance
	var definition ProcessDefinition
	j.Named("Deadline").
		Left(&instance, &t2.Instance).
		Left(&definition, j.OnCols("process_instances", "definition_id", &definition.ID)).
		Take(&t2.ID, "ID").
		Take(&t2.Name, "Name").
		Take(&t2.NodeID, "NodeID").
		Take(&t2.Status, "Status").
		Take(&t2.Priority, "Priority").
		Take(&t2.Assignee, "Assignee").
		Take(&t2.DueDate, "DueDate").
		Take(&definition.Key, "ProcessKey").
		Take(&definition.Name, "ProcessName").
		OrderAsc(&t2.DueDate).
		OrderAsc(&t2.ID)
}
