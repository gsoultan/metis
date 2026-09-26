package auth

import (
	"context"
	"strings"

	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/interceptors/contracts"
)

// AccessPolicy is the ABAC extension point. Implementations may evaluate
// attribute-based rules beyond simple role membership. Use AllowAll as the
// default no-op when ABAC is not required.
type AccessPolicy interface {
	// Allow returns true when the authenticated principal is permitted to
	// perform action on resource.
	Allow(ctx context.Context, roles []string, action, resource string) bool
}

// allowAllPolicy is the Null Object implementation of AccessPolicy.
// It always grants access and is safe to use as a default.
type allowAllPolicy struct{}

// NewAllowAllPolicy returns the default no-op AccessPolicy.
func NewAllowAllPolicy() AccessPolicy { return &allowAllPolicy{} }

func (*allowAllPolicy) Allow(_ context.Context, _ []string, _, _ string) bool { return true }

// rbacInterceptor enforces role-based access control at the endpoint layer.
// It optionally delegates to an AccessPolicy for attribute-based decisions.
type rbacInterceptor struct {
	requiredRoles []string
	policy        AccessPolicy
	action        string
	resource      string
}

// NewRBACInterceptor returns an EndpointInterceptor that requires the caller to
// hold at least one of requiredRoles and satisfies the given AccessPolicy.
// Pass NewAllowAllPolicy() when ABAC is not needed.
func NewRBACInterceptor(requiredRoles []string, policy AccessPolicy, action, resource string) contracts.EndpointInterceptor {
	return &rbacInterceptor{
		requiredRoles: requiredRoles,
		policy:        policy,
		action:        action,
		resource:      resource,
	}
}

// NewRequireRoles is a convenience factory for pure RBAC (no ABAC) enforcement.
func NewRequireRoles(roles ...string) contracts.EndpointInterceptor {
	return NewRBACInterceptor(roles, NewAllowAllPolicy(), "", "")
}

// Intercept refuses a caller nobody knows with ErrUnauthorized (401) and a
// known caller without the right with ErrForbidden (403).
//
// Both used to be 401. That tells a signed-in person their session is the
// problem, so they sign in again and are refused again, when what they need is
// somebody to grant them a role — which the 403 names.
func (i *rbacInterceptor) Intercept(next endpoint.Endpoint) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		roles, err := rolesFromContext(ctx)
		if err != nil {
			return nil, pkgauth.ErrUnauthorized
		}

		if !i.hasRequiredRole(roles) {
			return nil, apierr.Forbiddenf("this needs the %s role, which your account does not have; an administrator can grant it",
				roleChoice(i.requiredRoles))
		}

		if !i.policy.Allow(ctx, roles, i.action, i.resource) {
			return nil, apierr.Forbiddenf("your roles do not allow this")
		}

		return next(ctx, request)
	}
}

// roleChoice names the roles any one of which would do: "ADMIN", "ADMIN or
// DESIGNER", "ADMIN, DESIGNER or OPERATOR".
func roleChoice(roles []string) string {
	if len(roles) < 2 {
		return strings.Join(roles, "")
	}
	last := len(roles) - 1
	return strings.Join(roles[:last], ", ") + " or " + roles[last]
}

// hasRequiredRole returns true when at least one of the caller's roles matches
// one of the interceptor's required roles. An empty required-roles list allows
// any authenticated user.
func (i *rbacInterceptor) hasRequiredRole(callerRoles []string) bool {
	if len(i.requiredRoles) == 0 {
		return true
	}
	for _, required := range i.requiredRoles {
		// Case-insensitive: setup seeds "ADMIN" but tokens minted elsewhere may
		// carry "admin". A case mismatch here would silently deny a legitimate
		// administrator, which reads as a broken login rather than a policy
		// decision.
		if entities.HasRole(callerRoles, required) {
			return true
		}
	}
	return false
}

// rolesFromContext extracts the caller's roles from the context.
//
// Both strategies put an account there — the JWT strategy the account the token
// names, the OIDC strategy the account linked to the identity provider's
// subject — so the roles are always the ones an administrator granted it. A
// token's own roles claim is never read: anything else in the context is
// nobody this can vouch for.
func rolesFromContext(ctx context.Context) ([]string, error) {
	v := ctx.Value(pkgauth.UserContextKey)
	if v == nil {
		return nil, pkgauth.ErrUnauthorized
	}

	switch u := v.(type) {
	case entities.User:
		return u.Roles, nil
	case *entities.User:
		return u.Roles, nil
	default:
		return nil, pkgauth.ErrUnauthorized
	}
}
