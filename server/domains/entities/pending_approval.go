package entities

import (
	"time"

	"github.com/google/uuid"
)

// PendingApproval is what a caller is told when what they asked for was not
// done but sent to a second administrator: which request waits, who asked,
// until when, and why it needs somebody else.
type PendingApproval struct {
	RequestID   uuid.UUID              `json:"request_id"`
	Status      DeviationRequestStatus `json:"status"`
	RequestedBy string                 `json:"requested_by"`
	ExpiresAt   time.Time              `json:"expires_at"`
	// Because is a list always, empty rather than null.
	Because []string `json:"because"`
}

// PendingApprovalOf is what a caller is told about r.
func PendingApprovalOf(r DeviationRequest) PendingApproval {
	because := r.Because()
	if because == nil {
		because = []string{}
	}
	return PendingApproval{
		RequestID:   r.ID,
		Status:      r.Status,
		RequestedBy: r.RequestedBy,
		ExpiresAt:   r.ExpiresAt,
		Because:     because,
	}
}
