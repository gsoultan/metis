package entities

import (
	"time"

	"github.com/google/uuid"
)

// ApprovedRunReportWindow is how long an approved request waits for its run to
// report back. An approved request is in use until its deadline or until this
// long after it was approved, whichever comes first: a run that ends says so,
// and one whose process died says nothing, so silence has to end somewhere.
const ApprovedRunReportWindow = time.Hour

// planBecause is where a request's stored plan keeps the reasons it needs a
// second administrator.
const planBecause = "because"

// DeviationRequest is a deviation one administrator asked for and a second has
// to approve: a step of an instance waived in place, or a migration that skips
// a step or drops a control.
//
// It records what was asked (Command), what the person asking was shown
// (Plan), who decided and what became of it (Outcome). Plan is for reading: an
// approval plans again from Command and reads none of it.
//
// No JSON or ORM tags: an endpoint maps it to a view, and a repository maps it
// to a row.
type DeviationRequest struct {
	ID                   uuid.UUID
	CreatedAt, UpdatedAt time.Time
	Project              *Project

	Kind   DeviationRequestKind
	Status DeviationRequestStatus

	// Instance is the instance a waive is for; nil for a migration.
	Instance *ProcessInstance
	// SourceDefinition and TargetDefinition are the versions a migration moves
	// between.
	SourceDefinition, TargetDefinition *ProcessDefinition

	RequestedBy   string
	RequestedByID uuid.UUID
	Reason        string

	Command, Plan map[string]any

	// Fingerprint identifies what was asked for: one visit of one step, or one
	// migration policy. Two live requests never share one.
	Fingerprint string
	// ApprovedInstances are the instances the request showed. An approved
	// migration acts on no other.
	ApprovedInstances []uuid.UUID
	// ExpiresAt is fixed when the request is made.
	ExpiresAt time.Time

	DecidedBy      string
	DecidedByID    uuid.UUID
	DecisionReason string
	DecidedAt      *time.Time
	Outcome        map[string]any
}

// EffectiveStatus is the status the request has at now, whatever the sweep has
// recorded so far: the clock decides, and the sweep only writes it down.
//
// A request still pending at its deadline has expired. One still approved
// when its run window has closed — or approved at no recorded time — was
// interrupted. Anything else is as stored.
func (r DeviationRequest) EffectiveStatus(now time.Time) DeviationRequestStatus {
	switch {
	case r.Status == DeviationRequestPending && !now.Before(r.ExpiresAt):
		return DeviationRequestExpired
	case r.RunWindowClosed(now):
		return DeviationRequestInterrupted
	}
	return r.Status
}

// RunWindowClosed reports whether an approved request is out of use: its
// deadline has come, or ApprovedRunReportWindow has passed since the approval.
// Only an approved request has a run window.
//
// An approved request with no time of approval is out of use at once: a window
// that starts at no known moment cannot be said to be open, and absent
// constraint means deny. Nothing in the product writes one.
func (r DeviationRequest) RunWindowClosed(now time.Time) bool {
	if r.Status != DeviationRequestApproved {
		return false
	}
	if r.DecidedAt == nil {
		return true
	}
	return !now.Before(r.ExpiresAt) || !now.Before(r.DecidedAt.Add(ApprovedRunReportWindow))
}

// SelfApproved reports whether the person who asked is the one who approved:
// no second person did. Told from account ids, so renaming an account does not
// hide it, and only of an approval — the requester rejecting their own request
// is a withdrawal.
func (r DeviationRequest) SelfApproved() bool {
	if r.DecidedByID == uuid.Nil || r.DecidedByID != r.RequestedByID {
		return false
	}
	switch r.Status {
	case DeviationRequestApproved, DeviationRequestApplied, DeviationRequestInterrupted:
		return true
	}
	return false
}

// Because is why the request needs a second administrator, as its plan stored
// it: a list of sentences. Nil when the plan holds no such list.
func (r DeviationRequest) Because() []string {
	switch reasons := r.Plan[planBecause].(type) {
	case []string:
		return reasons
	case []any:
		out := make([]string, 0, len(reasons))
		for _, reason := range reasons {
			sentence, ok := reason.(string)
			if !ok {
				return nil
			}
			out = append(out, sentence)
		}
		return out
	}
	return nil
}
