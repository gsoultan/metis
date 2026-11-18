package engine

import (
	"context"

	"github.com/gsoultan/metis/internal/pkg/identity"
)

// Identity proves who a request is and holds every account's credentials, for
// a program that takes signing in over from the engine. See WithIdentity.
type Identity = identity.Identity

// Principal is who a request is, as an Identity proved it.
type Principal = identity.Principal

// Account is an engine account as an Identity sees it.
type Account = identity.Account

// Accounts reads the engine's accounts for an Identity.
type Accounts = identity.Accounts

// IdentityEnv is what the engine gives the Identity it builds: the database,
// its accounts and its at-rest encryption.
type IdentityEnv = identity.Env

// Option changes how an engine is built.
type Option func(*Engine)

// WithIdentity hands signing in to the program that embeds the engine.
//
// build is called once, after the database is open and migrated and before
// anything is served, by the server and by the maintenance commands alike. An
// error stops the engine from starting.
//
// With an Identity, the engine:
//   - admits a request to /api/v1 only as the Principal the Identity proves,
//     reading the account's roles itself, fresh, for every request;
//   - refuses a Principal that came through a key on the routes that manage
//     accounts, keys, organizations and the installation;
//   - serves none of a person's own credential routes (signing in, the
//     second-factor steps, changing one's own password, signing out): the
//     embedding program serves them;
//   - writes no password hash and no authenticator secret, calling the
//     Identity instead, and leaves the ones it has in place, so taking the
//     Identity away again still lets people sign in as before.
func WithIdentity(build func(ctx context.Context, env IdentityEnv) (Identity, error)) Option {
	return func(e *Engine) { e.app.UseIdentity(identity.Factory(build)) }
}
