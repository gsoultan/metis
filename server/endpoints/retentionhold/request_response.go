package retentionhold

import (
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// InstanceHoldRequest names the instance whose hold is read.
type InstanceHoldRequest struct {
	InstanceID string `json:"-"`
}

// InstanceHoldResponse is the hold on an instance, or none: Held says which,
// so an instance with no hold is an answer and not a 404.
type InstanceHoldResponse struct {
	Held bool      `json:"held"`
	Hold *HoldView `json:"hold,omitzero"`
	Err  error     `json:"err,omitzero"`
}

func (r InstanceHoldResponse) Failed() error { return r.Err }

// ChangeHoldRequest places or releases the hold on an instance, with the
// reason the instance's audit trail keeps.
type ChangeHoldRequest struct {
	// InstanceID is taken from the address, never from the body.
	InstanceID string `json:"-"`
	Reason     string `json:"reason"`
}

// ChangeHoldResponse is the hold that was placed, or the one released.
type ChangeHoldResponse struct {
	Hold *HoldView `json:"hold,omitzero"`
	Err  error     `json:"err,omitzero"`
}

func (r ChangeHoldResponse) Failed() error { return r.Err }

// ListHoldsRequest names the project whose holds are listed.
type ListHoldsRequest struct {
	ProjectID string `json:"-"`
}

// ListHoldsResponse is a project's holds, newest first.
type ListHoldsResponse struct {
	Holds []HoldView `json:"holds"`
	Err   error      `json:"err,omitzero"`
}

func (r ListHoldsResponse) Failed() error { return r.Err }

// HoldView is a hold as the API shows it.
type HoldView struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id"`
	InstanceID string    `json:"instance_id"`
	Reason     string    `json:"reason"`
	PlacedBy   string    `json:"placed_by"`
	PlacedAt   time.Time `json:"placed_at"`
}

// ViewOf maps a hold to its view.
func ViewOf(hold entities.RetentionHold) HoldView {
	view := HoldView{
		ID:       hold.ID.String(),
		Reason:   hold.Reason,
		PlacedBy: hold.PlacedBy,
		PlacedAt: hold.CreatedAt,
	}
	if hold.Project != nil {
		view.ProjectID = hold.Project.ID.String()
	}
	if hold.Instance != nil {
		view.InstanceID = hold.Instance.ID.String()
	}
	return view
}
