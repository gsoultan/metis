package contracts

import (
	"time"

	"github.com/google/uuid"
)

// MigrationApproval is the second administrator's approval a migration runs
// under: which request, who asked for it, who approved it and when.
//
// It is filled by the migration's own gate and by nothing else. An apply
// reads the request a caller names (WithApprovedRequest), checks it against
// the plan it is about to run, and writes what it verified here — over
// whatever was there. So a value a caller builds by hand is never acted on
// and never recorded: the gate replaces it, or refuses.
type MigrationApproval struct {
	RequestID     uuid.UUID
	RequestedBy   string
	RequestedByID uuid.UUID
	ApprovedBy    string
	ApprovedByID  uuid.UUID
	DecidedAt     time.Time
	// SelfApproved says the approver is the requester: no second person
	// approved it, which an installation allows in the organizations it
	// names as having one administrator.
	SelfApproved bool
	// Organization is the organization a self-approval was allowed in, as
	// the approval recorded it. Set only when SelfApproved is.
	Organization uuid.UUID
}

// Granted reports whether the approval names a request and the account that
// approved it. One that does not is no approval, whatever else it holds.
func (a MigrationApproval) Granted() bool {
	return a.RequestID != uuid.Nil && a.ApprovedByID != uuid.Nil
}

// WithApprovedRequest names the approved request a migration runs under. It
// sets the request's id and nothing else: who asked and who approved are read
// from the stored request by the apply, never taken from a caller.
func WithApprovedRequest(requestID uuid.UUID) MigrationOption {
	return func(o *MigrationOptions) { o.Approval = MigrationApproval{RequestID: requestID} }
}
