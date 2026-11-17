package entities

import (
	"time"

	"github.com/google/uuid"
)

// IntegrationAPIKey is a key an organization gave another system, as its
// administrators see it: never the key itself, which is shown once, when it is
// made.
type IntegrationAPIKey struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	Name           string     `json:"name"`
	Prefix         string     `json:"prefix"`
	Roles          []string   `json:"roles"`
	CreatedBy      string     `json:"created_by"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      *time.Time `json:"expires_at,omitzero"`
	LastUsedAt     *time.Time `json:"last_used_at,omitzero"`
	RevokedAt      *time.Time `json:"revoked_at,omitzero"`
}

// Live reports whether the key still opens anything at now: not revoked, and
// not past its expiry.
func (k IntegrationAPIKey) Live(now time.Time) bool {
	if k.RevokedAt != nil {
		return false
	}
	return k.ExpiresAt == nil || now.Before(*k.ExpiresAt)
}
