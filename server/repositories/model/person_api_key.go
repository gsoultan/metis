package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// PersonAPIKey lets a person's own system call the People task API as them,
// without their password.
//
// A person may hold several, each named for the system that uses it, so one
// can be revoked without breaking the others. Only the key's SHA-256 is kept:
// a backup or a `SELECT` must not be enough to act as somebody. Prefix is the
// key's first characters, kept so a list can say which key is which.
type PersonAPIKey struct {
	storm.Model

	Participant WorkflowUser

	Name       string
	Prefix     string
	TokenHash  string
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

func (k *PersonAPIKey) Schema(t *storm.Table) {
	t.Name("person_api_keys")
	t.Col(&k.Name).Size(100)
	t.Col(&k.Prefix).Size(16)
	t.Col(&k.TokenHash).Size(64)
	// A key is found by its hash on every request it makes.
	t.Index(&k.TokenHash).Unique().Named("ux_person_api_keys_token_hash")
}
