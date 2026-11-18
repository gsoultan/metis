// Package identity is the seam through which a program that embeds the engine
// takes over who people are: signing in, sessions, second factors and keys.
//
// The engine keeps the accounts — usernames, organizations, roles — because
// tasks, assignments and the audit trail point at them. What proves a person
// is that account moves to the Identity: with one set, the engine neither
// reads nor writes a password hash or an authenticator secret, and serves none
// of the routes a person uses on their own credentials.
//
// The public names are in package engine; this package exists so the engine's
// own internals can refer to them without importing it.
package identity

import (
	"context"
	"database/sql"
	"net/http"
)

// Principal is who a request is, as the Identity proved it.
type Principal struct {
	// UserID is the engine account's id.
	UserID string
	// Roles are the roles the Identity saw, by organization id. The engine
	// decides with its own copy, read fresh for each request; these are for
	// the Identity's own use and for logs.
	Roles map[string][]string
	// ViaKey is true when the request carried an integration key rather than
	// a person's session. Such a request is refused on the routes that manage
	// accounts, keys, organizations and the installation, whatever its roles.
	ViaKey bool
}

// Identity proves who a request is, and holds every account's credentials.
type Identity interface {
	// Authenticate reads the request's credentials. It answers false with a
	// nil error when they name nobody, which the engine answers with 401, and
	// an error when it cannot tell, which the engine answers with 503: a
	// failing identity store never makes a caller anonymous.
	Authenticate(ctx context.Context, r *http.Request) (Principal, bool, error)
	// SetPassword replaces the account's password and ends its sessions. The
	// engine calls it wherever it makes or manages an account: setting up the
	// first administrator, creating an account, an administrator's reset and
	// --reset-password.
	SetPassword(ctx context.Context, userID, plain string) error
	// RemoveSecondFactor removes the account's authenticator app and ends its
	// sessions, for an administrator's reset and --reset-two-factor.
	RemoveSecondFactor(ctx context.Context, userID string) error
}

// Account is an engine account as the Identity sees it.
type Account struct {
	UserID   string
	Username string
	// Roles by organization id.
	Roles map[string][]string
	// Disabled accounts must not be signed in.
	Disabled bool
}

// Accounts reads the engine's accounts, so the Identity never reads the
// engine's tables.
type Accounts interface {
	ByUsername(ctx context.Context, username string) (Account, bool, error)
	ByID(ctx context.Context, userID string) (Account, bool, error)
}

// Env is what the engine gives the Identity it builds.
type Env struct {
	// DB is the installation's main database, through a pool of its own that
	// the engine closes when it stops. The Identity's tables are named
	// identity_*, so they never meet the engine's.
	DB *sql.DB
	// Accounts reads the engine's accounts.
	Accounts Accounts
	// Seal and Open are the engine's at-rest encryption, under ENCRYPTION_KEY.
	// Open also reads what was sealed under ENCRYPTION_KEY_PREVIOUS.
	Seal func(plain string) (string, error)
	Open func(sealed string) (string, error)
}

// Factory builds the Identity once the database is open and migrated. An error
// stops the engine from starting.
type Factory func(ctx context.Context, env Env) (Identity, error)
