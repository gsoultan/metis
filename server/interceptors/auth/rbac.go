package auth

import (
	"context"
	"strings"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
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

	// globalOnly counts only the roles the caller holds in every
	// organization, for what no one organization owns. See
	// NewRequireGlobalRoles.
	globalOnly bool
}

// NewRBACInterceptor returns an EndpointInterceptor that requires the caller to
// hold at least one of requiredRoles and satisfies the given AccessPolicy.
// Pass NewAllowAllPolicy() when ABAC is not needed.
//
// The roles that count are the caller's global ones and the ones it holds in
// the organization the request is for — the tenant resolver's answer, so it
// belongs after the resolver. A request that is for no organization counts the
// global roles alone.
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

// NewRequireGlobalRoles is NewRequireRoles for what every organization on the
// installation shares — its connectors, its platform accounts, its list of
// organizations. Only a role the caller holds in every organization counts:
// one granted in a single organization was granted by that organization's
// administrators, and must not reach past it, whichever organization the
// request happens to be for.
func NewRequireGlobalRoles(roles ...string) contracts.EndpointInterceptor {
	return &rbacInterceptor{requiredRoles: roles, policy: NewAllowAllPolicy(), globalOnly: true}
}

// Intercept refuses a caller nobody knows with ErrUnauthorized (401) and a
// known caller without the right with ErrForbidden (403).
//
// Both used to be 401. That tells a signed-in person their session is the
// problem, so they sign in again and are refused again, when what they need is
// somebody to grant them a role — which the 403 names.
func (i *rbacInterceptor) Intercept(next endpoint.Endpoint) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		caller, err := callerFromContext(ctx)
		if err != nil {
			return nil, pkgauth.ErrUnauthorized
		}

		organization := i.organizationOf(ctx)
		if !i.hasRequiredRole(caller, organization) {
			return nil, i.refusal(organization)
		}

		if !i.policy.Allow(ctx, caller.RolesIn(organization), i.action, i.resource) {
			return nil, apierr.Forbiddenf("your roles do not allow this")
		}

		return next(ctx, request)
	}
}

// organizationOf is the organization whose roles count beside the caller's
// global ones: the one the request is for, or none for a global gate.
func (i *rbacInterceptor) organizationOf(ctx context.Context) uuid.UUID {
	if i.globalOnly {
		return uuid.Nil
	}
	return entities.ActingOrganization(ctx)
}

// refusal names the roles that would do, and where they would have to be held.
func (i *rbacInterceptor) refusal(organization uuid.UUID) error {
	wanted := roleChoice(i.requiredRoles)
	switch {
	case i.globalOnly:
		return apierr.Forbiddenf("this needs the %s role in every organization, which your account does not have; "+
			"a role held in one organization does not reach what every organization shares", wanted)
	case organization != uuid.Nil:
		return apierr.Forbiddenf("this needs the %s role, which your account does not hold in this organization; "+
			"an administrator here can grant it", wanted)
	default:
		return apierr.Forbiddenf("this needs the %s role, which your account does not have; an administrator can grant it",
			wanted)
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

// hasRequiredRole returns true when the caller holds at least one of the
// interceptor's required roles in the organization — globally, or there. An
// empty required-roles list allows any authenticated user.
//
// Case-insensitive, through HasRole: setup seeds "ADMIN" but tokens minted
// elsewhere may carry "admin". A case mismatch here would silently deny a
// legitimate administrator, which reads as a broken login rather than a policy
// decision.
func (i *rbacInterceptor) hasRequiredRole(caller entities.User, organization uuid.UUID) bool {
	if len(i.requiredRoles) == 0 {
		return true
	}
	for _, required := range i.requiredRoles {
		if caller.HoldsRoleIn(organization, required) {
			return true
		}
	}
	return false
}

// callerFromContext returns the account the request is from.
//
// Both strategies put an account there — the JWT strategy the account the token
// names, the OIDC strategy the account linked to the identity provider's
// subject — so the roles are always the ones an administrator granted it, read
// from the account when the request was authenticated. A token's own roles
// claim is never read: anything else in the context is nobody this can vouch
// for.
func callerFromContext(ctx context.Context) (entities.User, error) {
	switch u := ctx.Value(pkgauth.UserContextKey).(type) {
	case entities.User:
		return u, nil
	case *entities.User:
		if u != nil {
			return *u, nil
		}
	}
	return entities.User{}, pkgauth.ErrUnauthorized
}
