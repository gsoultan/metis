package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// IntegrationAPIKey lets another system call the platform API on behalf of an
// organization, without anybody's password and without a second factor.
//
// A key belongs to the organization, not to whoever made it: it keeps working
// when that person leaves, and an administrator of the organization can see
// and revoke every key it has. It acts with the roles it was given, in that
// organization alone.
//
// Only the key's SHA-256 is kept: a backup or a `SELECT` must not be enough to
// act as the organization. Prefix is the key's first characters, kept so a
// list can say which key is which. CreatedBy is the username of the
// administrator who made it, as it was then.
type IntegrationAPIKey struct {
	storm.Model

	Organization Organization

	Name       string
	Prefix     string
	TokenHash  string
	Roles      storm.JSON
	CreatedBy  string
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

func (k *IntegrationAPIKey) Schema(t *storm.Table) {
	t.Name("integration_api_keys")
	t.Col(&k.Organization).OnDelete(storm.Cascade)
	t.Col(&k.Name).Size(100)
	t.Col(&k.Prefix).Size(16)
	t.Col(&k.TokenHash).Size(64)
	t.Col(&k.Roles).Default(emptyJSONList)
	t.Col(&k.CreatedBy).Size(255)
	// A key is found by its hash on every request it makes.
	t.Index(&k.TokenHash).Unique().Named("ux_integration_api_keys_token_hash")
	// An organization's keys are listed together.
	t.Index(&k.Organization).Named("ix_integration_api_keys_organization_id")
}
