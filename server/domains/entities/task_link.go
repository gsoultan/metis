package entities

import (
	"time"

	"github.com/google/uuid"
)

// TaskLink lets one person act on one task, once, by opening a link.
//
// The token itself is never kept: whoever holds the link holds the
// credential, so only its hash is stored, and a TaskLink read back carries
// neither.
type TaskLink struct {
	ID            uuid.UUID  `json:"id"`
	ProjectID     uuid.UUID  `json:"project_id"`
	TaskID        uuid.UUID  `json:"task_id"`
	ParticipantID uuid.UUID  `json:"participant_id"`
	ExpiresAt     time.Time  `json:"expires_at"`
	UsedAt        *time.Time `json:"used_at,omitzero"`
	VoidedAt      *time.Time `json:"voided_at,omitzero"`
	CreatedAt     time.Time  `json:"created_at"`
}

// GoodAt reports whether the link can still be used at now: not used, not
// voided and not expired. Whether the task and the person still allow it is
// asked of them, not of the link.
func (l TaskLink) GoodAt(now time.Time) bool {
	return l.UsedAt == nil && l.VoidedAt == nil && now.Before(l.ExpiresAt)
}
