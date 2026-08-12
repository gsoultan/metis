package entities

import (
	"time"

	"github.com/google/uuid"
)

// Task represents a user task or an activity in a process instance.
type Task struct {
	ID              uuid.UUID        `json:"id"`
	Project         *Project         `json:"project,omitzero"`
	Instance        *ProcessInstance `json:"instance,omitzero"`
	Node            *Node            `json:"node,omitzero"`
	Name            string           `json:"name"`
	Description     string           `json:"description,omitzero"`
	Type            NodeType         `json:"type"`
	Status          TaskStatus       `json:"status"` // e.g., "unclaimed", "claimed", "completed"
	Assignee        *User            `json:"assignee,omitzero"`
	CandidateUsers  []*User          `json:"candidate_users,omitzero"`
	CandidateGroups []*Group         `json:"candidate_groups,omitzero"`
	Priority        int              `json:"priority,omitzero"`
	DueDate         *time.Time       `json:"due_date,omitzero"`
	FormKey         string           `json:"form_key,omitzero"`
	FormDefinition  string           `json:"form_definition,omitzero"`
	Variables       map[string]any   `json:"variables,omitzero"`
	CreatedAt       time.Time        `json:"created_at,omitzero"`
}

// NodeID returns the BPMN node ID this task was created for, or "" when the
// task carries no node reference. Callers routinely need the ID rather than the
// whole node, and the relation is a pointer, so this keeps the nil check in one
// place instead of at every call site.
func (t Task) NodeID() string {
	if t.Node == nil {
		return ""
	}
	return t.Node.ID
}

// AssigneeUsername returns the username of the task assignee, or "" when the
// task is unassigned.
func (t Task) AssigneeUsername() string {
	if t.Assignee == nil {
		return ""
	}
	return t.Assignee.Username
}
