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
	// ProcessCancelled is an instance somebody ended deliberately. See
	// entities.ProcessCancelled for why it is not "completed".
	ProcessCancelled ProcessStatus = "cancelled"
)

// ProcessStatuses is every state an instance may be in.
//
// Exported so the edge can refuse a status it does not recognise instead of
// passing it down to a predicate that matches nothing. A filter that quietly
// matches nothing reads as "this project is empty"; one that quietly matches
// everything reads as "nothing is wrong". Both are worse than an error.
var ProcessStatuses = []ProcessStatus{
	ProcessActive,
	ProcessCompleted,
	ProcessSuspended,
	ProcessFailed,
	ProcessCancelled,
}

// ValidProcessStatus reports whether s names a state an instance can be in.
func ValidProcessStatus(s ProcessStatus) bool {
	for _, known := range ProcessStatuses {
		if s == known {
			return true
		}
	}
	return false
}

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
	ProjectID        UUID                   `json:"project_id,omitzero"`
	Project          ProjectModel           `json:"project,omitzero"`
	DefinitionID     UUID                   `json:"definition_id,omitzero"`
	Definition       ProcessDefinitionModel `json:"definition,omitzero"`
	ParentInstanceID *UUID                  `json:"parent_instance_id,omitzero"`
	ParentNodeID     string                 `json:"parent_node_id,omitzero"`
	Status           ProcessStatus          `json:"status"`
	BusinessKey      string                 `json:"business_key,omitzero"`
	Variables        EncryptedMap           `json:"variables,omitzero"`
	Tokens           []Token                `json:"tokens,omitzero"`
	CompletedNodes   []string               `json:"completed_nodes,omitzero"`
	CompensatedNodes []string               `json:"compensated_nodes,omitzero"`
	// MultiInstance is engine bookkeeping for nodes that run once per item. It
	// has its own column so it cannot collide with business variables or reach
	// the audit trail through them.
	MultiInstance map[string]MultiInstanceStateModel `json:"multi_instance,omitzero"`
	// Joins counts the branches that have reached each waiting gateway. Its own
	// column for the same reason as MultiInstance.
	Joins map[string]int `json:"joins,omitzero"`
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
