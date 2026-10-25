package entities

import (
	"time"

	"github.com/google/uuid"
)

// PersonAPIKey is a key a person gave one of their systems, as they see it:
// never the key itself, which is shown once, when it is made.
type PersonAPIKey struct {
	ID            uuid.UUID  `json:"id"`
	ParticipantID uuid.UUID  `json:"-"`
	Name          string     `json:"name"`
	Prefix        string     `json:"prefix"`
	CreatedAt     time.Time  `json:"created_at"`
	LastUsedAt    *time.Time `json:"last_used_at,omitzero"`
	RevokedAt     *time.Time `json:"revoked_at,omitzero"`
}
