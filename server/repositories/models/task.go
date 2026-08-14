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
	ProjectID       UUID         `gorm:"index" json:"project_id,omitzero"`
	InstanceID      UUID         `gorm:"index" json:"instance_id,omitzero"`
	NodeID          string       `json:"node_id"`
	Name            string       `json:"name"`
	Description     string       `json:"description,omitzero"`
	Type            NodeType     `gorm:"index" json:"type"`
	Status          TaskStatus   `gorm:"index" json:"status"`
	Assignee        string       `gorm:"size:255;index" json:"assignee,omitzero"`
	CandidateUsers  []string     `gorm:"type:text;serializer:json" json:"candidate_users,omitzero"`
	CandidateGroups []string     `gorm:"type:text;serializer:json" json:"candidate_groups,omitzero"`
	Priority        int          `json:"priority,omitzero"`
	DueDate         *time.Time   `json:"due_date,omitzero"`
	FormKey         string       `json:"form_key,omitzero"`
	FormDefinition  string       `json:"form_definition,omitzero"`
	Variables       EncryptedMap `gorm:"type:text" json:"variables,omitzero"`
}

// TableName overrides the table name for TaskModel.
func (TaskModel) TableName() string {
	return "tasks"
}
