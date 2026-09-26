package entities

import (
	"time"

	"github.com/google/uuid"
)

// User represents a user in the system.
type User struct {
	ID            uuid.UUID       `json:"id"`
	Organizations []*Organization `json:"organizations,omitzero"`
	Projects      []*Project      `json:"projects,omitzero"`
	Username      string          `json:"username"`
	FullName      string          `json:"full_name"`
	DisplayName   string          `json:"display_name"`
	Organization  *Organization   `json:"organization,omitzero"`
	Email         string          `json:"email"`

	// Roles are the account's global roles: it holds them in every
	// organization it belongs to.
	Roles     []string  `json:"roles"`
	CreatedAt time.Time `json:"created_at,omitzero"`

	// RolesByOrganization are the roles the account holds in one organization
	// alone, by organization: granted there, and acting only in a request for
	// it. RolesIn says what the account acts with in an organization.
	//
	// Never written out. The account is listed to every organization it
	// belongs to, and what it holds in one of them is not the others' to read.
	RolesByOrganization map[uuid.UUID][]string `json:"-"`

	// IdentityProvider is the issuer this account signs in through. Empty for
	// a local account, which signs in with a password held here; set, the
	// password lives at the provider and nothing here can change it.
	IdentityProvider string `json:"identity_provider,omitzero"`
}
