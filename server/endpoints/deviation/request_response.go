package deviation

import (
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

type ListInstanceDeviationsRequest struct {
	InstanceID string `json:"instanceId"`
}

type ListInstanceDeviationsResponse struct {
	Deviations []DeviationView `json:"deviations"`
	Err        error           `json:"err,omitzero"`
}

func (r ListInstanceDeviationsResponse) Failed() error { return r.Err }

// DeviationView is a deviation as a member of the organization reads it. It
// carries names, never account ids: an account id identifies a person inside
// the ledger across renames, and a reader has no use for it.
type DeviationView struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	Scope       string         `json:"scope"`
	Origin      string         `json:"origin"`
	Status      string         `json:"status"`
	NodeID      string         `json:"node_id,omitzero"`
	NodeName    string         `json:"node_name,omitzero"`
	TaskID      string         `json:"task_id,omitzero"`
	IterationID string         `json:"iteration_id,omitzero"`
	Actor       string         `json:"actor"`
	Reason      string         `json:"reason,omitzero"`
	ApprovedBy  string         `json:"approved_by,omitzero"`
	Before      map[string]any `json:"before,omitzero"`
	After       map[string]any `json:"after,omitzero"`
	Details     map[string]any `json:"details,omitzero"`
	RunID       string         `json:"run_id,omitzero"`
	RequestID   string         `json:"request_id,omitzero"`
	AuditID     string         `json:"audit_entry_id,omitzero"`
	CreatedAt   time.Time      `json:"created_at"`
	DecidedAt   *time.Time     `json:"decided_at,omitzero"`
}

// ViewOf maps a deviation to what the route returns.
func ViewOf(d entities.Deviation) DeviationView {
	view := DeviationView{
		ID: idString(d.ID), Kind: string(d.Kind), Scope: string(d.Scope), Origin: string(d.Origin),
		Status: string(d.Status), IterationID: d.IterationID, Actor: d.Actor, Reason: d.Reason,
		ApprovedBy: d.ApprovedBy, Before: d.Before, After: d.After, Details: d.Details,
		RunID: idString(d.RunID), RequestID: idString(d.RequestID), AuditID: idString(d.AuditEntryID),
		CreatedAt: d.CreatedAt, DecidedAt: d.DecidedAt,
	}
	if d.Node != nil {
		view.NodeID, view.NodeName = d.Node.ID, d.Node.Name
	}
	if d.Task != nil {
		view.TaskID = idString(d.Task.ID)
	}
	return view
}

// idString is "" for no id, so the field is omitted rather than a nil UUID.
func idString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}
