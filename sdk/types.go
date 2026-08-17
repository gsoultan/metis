package gobpm

import "time"

// The types here are deliberately leaner than the server's: they carry the
// fields an integrating application acts on, and unknown fields in responses
// are ignored, so newer servers keep working with older SDKs.

// PageInfo reports where a listing sits in its full result set.
type PageInfo struct {
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	HasMore  bool  `json:"has_more"`
}

// DefinitionRef identifies a deployed process definition.
type DefinitionRef struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// NodeRef identifies one node of a definition — the BPMN element ID plus the
// human name the designer gave it.
type NodeRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// ProcessInstance is one running (or finished) execution of a definition.
type ProcessInstance struct {
	ID         string         `json:"id"`
	Status     string         `json:"status"` // active | completed | suspended | failed
	Definition *DefinitionRef `json:"definition,omitempty"`
	Variables  Variables      `json:"variables,omitempty"`
	CreatedAt  time.Time      `json:"created_at,omitzero"`
}

// UserRef identifies a user well enough to display.
type UserRef struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
}

// Task is a human task waiting in someone's inbox.
type Task struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Status      string           `json:"status"` // unclaimed | claimed | completed | ...
	Priority    int              `json:"priority,omitempty"`
	DueDate     *time.Time       `json:"due_date,omitempty"`
	FormKey     string           `json:"form_key,omitempty"`
	Assignee    *UserRef         `json:"assignee,omitempty"`
	Node        *NodeRef         `json:"node,omitempty"`
	Instance    *ProcessInstance `json:"instance,omitempty"`
	Variables   Variables        `json:"variables,omitempty"`
}

// ExternalTask is one unit of work published for an external worker.
type ExternalTask struct {
	ID              string           `json:"id"`
	Topic           string           `json:"topic"`
	Retries         int              `json:"retries,omitempty"`
	LockExpiration  *time.Time       `json:"lock_expiration,omitempty"`
	Node            *NodeRef         `json:"node,omitempty"`
	ProcessInstance *ProcessInstance `json:"process_instance,omitempty"`
	Variables       Variables        `json:"variables,omitempty"`
}

// AuditEntry is one line of an instance's business timeline.
type AuditEntry struct {
	Type      string    `json:"type"`
	NodeID    string    `json:"node_id,omitempty"`
	NodeName  string    `json:"node_name,omitempty"`
	Message   string    `json:"message"`
	Narrative string    `json:"narrative,omitempty"`
	CreatedAt time.Time `json:"created_at,omitzero"`
}
