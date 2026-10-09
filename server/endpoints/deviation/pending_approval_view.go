package deviation

import (
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// PendingApprovalView is what a caller is told when what they asked for was
// not done but sent to a second administrator: which request waits, who
// asked, until when, and why it needs somebody else.
//
// It names the requester and carries no account id, as every view here does.
type PendingApprovalView struct {
	RequestID   string    `json:"request_id"`
	Status      string    `json:"status"`
	RequestedBy string    `json:"requested_by"`
	ExpiresAt   time.Time `json:"expires_at"`
	// Because is a list always, empty rather than null.
	Because []string `json:"because"`
}

// PendingApprovalViewOf maps what a service answered about a waiting request
// to what a route returns.
func PendingApprovalViewOf(p entities.PendingApproval) PendingApprovalView {
	return PendingApprovalView{
		RequestID:   idString(p.RequestID),
		Status:      string(p.Status),
		RequestedBy: p.RequestedBy,
		ExpiresAt:   p.ExpiresAt,
		Because:     listOf(p.Because),
	}
}
