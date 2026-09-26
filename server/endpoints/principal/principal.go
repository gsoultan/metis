// Package principal answers "who is calling?" for endpoints.
//
// The answer comes from the verified token the auth interceptor placed in the
// context, never from the request body. The task endpoints used to take the
// acting user from a `user_id` field on the wire, and the service then checked
// that *that* name was the assignee — so any signed-in member could complete a
// colleague's task by naming the colleague, and the audit trail recorded the
// colleague as having done it. In an orchestrator that executes approvals and
// payments, the actor is not a parameter.
//
// Whichever way somebody signed in, what the context holds is an account. An
// identity provider's sign-in is resolved to the account linked to it before
// any endpoint runs, so its claims are never the caller here.
package principal

import (
	"context"

	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
)

// Username returns the signed-in caller's username: their account's, which is
// what the audit trail names them by. An anonymous request is refused — there
// is no actor to attribute work to.
func Username(ctx context.Context) (string, error) {
	if u, ok := LocalUser(ctx); ok {
		return u.Username, nil
	}
	return "", pkgauth.ErrUnauthorized
}

// LocalUser returns the account the caller signed in as.
//
// It is the source for anything that needs more than a name: the account ID
// (for group membership), the roles, the organizations. "Local" is the account
// held here, as against a token's claims — somebody signed in through an
// identity provider has one too, the account linked to them.
func LocalUser(ctx context.Context) (entities.User, bool) {
	switch u := ctx.Value(pkgauth.UserContextKey).(type) {
	case entities.User:
		return u, u.Username != ""
	case *entities.User:
		if u != nil {
			return *u, u.Username != ""
		}
	}
	return entities.User{}, false
}

// LocalUserID returns the signed-in account's ID, or uuid.Nil.
func LocalUserID(ctx context.Context) uuid.UUID {
	if u, ok := LocalUser(ctx); ok {
		return u.ID
	}
	return uuid.Nil
}

// HasRole reports whether the caller carries the role. Absent principal,
// absent roles: false. Absent constraint means deny.
//
// Case is ignored, through entities.HasRole, because that is how the role
// check on every administrator-only endpoint compares — and accounts written
// by the older role picker hold "admin". Compared exactly here, the same
// administrator passed the endpoint's gate and was refused the override
// inside it.
func HasRole(ctx context.Context, role string) bool {
	var roles []string
	switch u := ctx.Value(pkgauth.UserContextKey).(type) {
	case entities.User:
		roles = u.Roles
	case *entities.User:
		if u != nil {
			roles = u.Roles
		}
	}
	return entities.HasRole(roles, role)
}
