// Package principal answers "who is calling?" for endpoints.
//
// The answer comes from the verified token the auth interceptor placed in the
// context, never from the request body. The task endpoints used to take the
// acting user from a `user_id` field on the wire, and the service then checked
// that *that* name was the assignee — so any signed-in member could complete a
// colleague's task by naming the colleague, and the audit trail recorded the
// colleague as having done it. In an orchestrator that executes approvals and
// payments, the actor is not a parameter.
package principal

import (
	"context"

	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
)

// Username returns the signed-in caller's username.
//
// An OIDC principal has no local account; its preferred username is used, and
// failing that the subject, so the audit trail still names someone stable.
// An anonymous request is refused — there is no actor to attribute work to.
func Username(ctx context.Context) (string, error) {
	switch u := ctx.Value(pkgauth.UserContextKey).(type) {
	case entities.User:
		if u.Username != "" {
			return u.Username, nil
		}
	case *entities.User:
		if u != nil && u.Username != "" {
			return u.Username, nil
		}
	case pkgauth.UserClaims:
		return claimsName(u)
	case *pkgauth.UserClaims:
		if u != nil {
			return claimsName(*u)
		}
	}
	return "", pkgauth.ErrUnauthorized
}

func claimsName(c pkgauth.UserClaims) (string, error) {
	if c.Username != "" {
		return c.Username, nil
	}
	if c.Subject != "" {
		return c.Subject, nil
	}
	return "", pkgauth.ErrUnauthorized
}

// LocalUser returns the signed-in local account, when there is one.
//
// It is the source for anything that needs more than a name: the account ID
// (for group membership), the roles, the organizations. An OIDC principal
// returns false; callers must then fall back to name-only behaviour rather
// than guess.
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

// LocalUserID returns the signed-in local account's ID, or uuid.Nil.
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
	case pkgauth.UserClaims:
		roles = u.Roles
	case *pkgauth.UserClaims:
		if u != nil {
			roles = u.Roles
		}
	}
	return entities.HasRole(roles, role)
}
