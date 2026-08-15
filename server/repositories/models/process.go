package models

import (
	"time"
)

// ProcessStatus defines the current state of a process instance in the database.
type ProcessStatus string

const (
	ProcessActive    ProcessStatus = "active"
	ProcessCompleted ProcessStatus = "completed"
	ProcessSuspended ProcessStatus = "suspended"
	ProcessFailed    ProcessStatus = "failed"
)

// TokenStatus represents the current state of a token in the database.
type TokenStatus string

const (
	TokenActive    TokenStatus = "active"
	TokenSuspended TokenStatus = "suspended"
	TokenCompleted TokenStatus = "completed"
)

// Token represents a single point of execution in a process instance in the database.
type Token struct {
	ID          UUID           `json:"id"`
	InstanceID  UUID           `json:"instance_id"`
	NodeID      string         `json:"node_id"`
	Status      TokenStatus    `json:"status"`
	IterationID string         `json:"iteration_id,omitzero"`
	Variables   map[string]any `json:"variables,omitzero"`
	CreatedAt   time.Time      `json:"created_at,omitzero"`
}

// ProcessInstanceModel represents the GORM model for process instances.
type ProcessInstanceModel struct {
	Base
	ProjectID        UUID                   `gorm:"index" json:"project_id,omitzero"`
	Project          ProjectModel           `gorm:"foreignKey:ProjectID" json:"project,omitzero"`
	DefinitionID     UUID                   `gorm:"index" json:"definition_id,omitzero"`
	Definition       ProcessDefinitionModel `gorm:"foreignKey:DefinitionID" json:"definition,omitzero"`
	ParentInstanceID *UUID                  `gorm:"index" json:"parent_instance_id,omitzero"`
	ParentNodeID     string                 `json:"parent_node_id,omitzero"`
	Status           ProcessStatus          `gorm:"index" json:"status"`
	Variables        EncryptedMap           `gorm:"type:text" json:"variables,omitzero"`
	Tokens           []Token                `gorm:"type:text;serializer:json" json:"tokens,omitzero"`
	CompletedNodes   []string               `gorm:"type:text;serializer:json" json:"completed_nodes,omitzero"`
	CompensatedNodes []string               `gorm:"type:text;serializer:json" json:"compensated_nodes,omitzero"`
	// MultiInstance is engine bookkeeping for nodes that run once per item. It
	// has its own column so it cannot collide with business variables or reach
	// the audit trail through them.
	MultiInstance map[string]MultiInstanceStateModel `gorm:"type:text;serializer:json" json:"multi_instance,omitzero"`
	// Joins counts the branches that have reached each waiting gateway. Its own
	// column for the same reason as MultiInstance.
	Joins map[string]int `gorm:"type:text;serializer:json" json:"joins,omitzero"`
}

// TableName overrides the table name for ProcessInstanceModel.
func (ProcessInstanceModel) TableName() string {
	return "process_instances"
}

// MultiInstanceStateModel is the stored form of a multi-instance node's progress.
type MultiInstanceStateModel struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
}
