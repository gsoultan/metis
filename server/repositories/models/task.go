package models

import (
	"time"
)

// TaskStatus defines the current state of a task in the database.
type TaskStatus string

const (
	TaskUnclaimed TaskStatus = "unclaimed" // same as pending
	TaskClaimed   TaskStatus = "claimed"
	TaskCompleted TaskStatus = "completed"
	TaskCanceled  TaskStatus = "canceled"
	TaskDelegated TaskStatus = "delegated"
	TaskEscalated TaskStatus = "escalated"
)

// TaskModel represents the GORM model for tasks.
type TaskModel struct {
	Base
	ProjectID  UUID   `gorm:"index" json:"project_id,omitzero"`
	InstanceID UUID   `gorm:"index" json:"instance_id,omitzero"`
	NodeID     string `json:"node_id"`
	// IterationID names which run of a multi-instance step this task is for
	// ("0", "1", …). Empty — NULL in the table — on a task of a step that runs
	// once, and on every task created before migration 31.
	IterationID string     `json:"iteration_id,omitzero"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitzero"`
	Type        NodeType   `gorm:"index" json:"type"`
	Status      TaskStatus `gorm:"index" json:"status"`
	Assignee    string     `gorm:"size:255;index" json:"assignee,omitzero"`
	// Owner is who delegated the task and waits for it back: set when its
	// holder delegates it, kept when the delegate hands it back, and cleared
	// by any other hand-over, a release and a migration. Empty for a task
	// never delegated. Indexed under the name the storm model gives the
	// index, so the two layers describe one index rather than two; migration
	// 32 adds both columns and the index to an installation that predates them.
	Owner string `gorm:"size:255;index:ix_tasks_owner" json:"owner,omitzero"`
	// DelegationState is "pending" while a delegate works the task and
	// "resolved" once they have handed it back.
	DelegationState string   `gorm:"size:32" json:"delegation_state,omitzero"`
	CandidateUsers  []string `gorm:"type:text;serializer:json" json:"candidate_users,omitzero"`
	CandidateGroups []string `gorm:"type:text;serializer:json" json:"candidate_groups,omitzero"`
	// not null, default 0: the storm scanner decodes this column as a
	// fixed-width integer, so a NULL is eight bytes it reads out of an empty
	// buffer — a panic on the read, not a zero. AutoMigrate does not carry the
	// model's non-nullability over on its own, which is what let GET
	// /api/v1/tasks drop the connection on a task somebody bulk-inserted.
	// Migration 21 applies the same thing to tables that already exist.
	Priority       int          `gorm:"not null;default:0" json:"priority,omitzero"`
	DueDate        *time.Time   `json:"due_date,omitzero"`
	FormKey        string       `json:"form_key,omitzero"`
	FormDefinition string       `json:"form_definition,omitzero"`
	Variables      EncryptedMap `gorm:"type:text" json:"variables,omitzero"`
}

// TableName overrides the table name for TaskModel.
func (TaskModel) TableName() string {
	return "tasks"
}
