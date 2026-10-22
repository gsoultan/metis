package operationlog

import (
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// ListRequest is the audit log's filter, as the query string carries it.
// Every field is optional.
type ListRequest struct {
	Actor     string
	Type      string
	ProjectID string
	From      string
	To        string
	Before    string
	Limit     string
}

// OperationView is one thing somebody did.
type OperationView struct {
	ID         string    `json:"id"`
	At         time.Time `json:"at"`
	Actor      string    `json:"actor"`
	Type       string    `json:"type"`
	ProjectID  string    `json:"project_id,omitzero"`
	InstanceID string    `json:"instance_id,omitzero"`
	NodeID     string    `json:"node_id,omitzero"`
	NodeName   string    `json:"node_name,omitzero"`
	Narrative  string    `json:"narrative"`
}

// ListResponse is a page of the log, newest first. Next continues it, and is
// absent on the last page.
type ListResponse struct {
	Operations []OperationView `json:"operations"`
	Next       string          `json:"next,omitzero"`
	Err        error           `json:"err,omitzero"`
}

func (r ListResponse) Failed() error { return r.Err }

// ViewOf is an entry as the log shows it.
func ViewOf(entry entities.AuditEntry) OperationView {
	view := OperationView{
		ID:        entry.ID.String(),
		At:        entry.Timestamp,
		Actor:     entry.Actor,
		Type:      entry.Type,
		Narrative: entry.Narrative,
	}
	if view.Narrative == "" {
		view.Narrative = entry.Message
	}
	if entry.Project != nil && entry.Project.ID != uuid.Nil {
		view.ProjectID = entry.Project.ID.String()
	}
	if entry.Instance != nil && entry.Instance.ID != uuid.Nil {
		view.InstanceID = entry.Instance.ID.String()
	}
	if entry.Node != nil {
		view.NodeID, view.NodeName = entry.Node.ID, entry.Node.Name
	}
	return view
}
