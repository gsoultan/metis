package auth

import (
	"context"

	"github.com/go-kit/kit/endpoint"
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

func (i *rbacInterceptor) Intercept(next endpoint.Endpoint) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		roles, err := rolesFromContext(ctx)
		if err != nil {
			return nil, pkgauth.ErrUnauthorized
		}

		if !i.hasRequiredRole(roles) {
			return nil, pkgauth.ErrUnauthorized
		}

		if !i.policy.Allow(ctx, roles, i.action, i.resource) {
			return nil, pkgauth.ErrUnauthorized
		}

		return next(ctx, request)
	}
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
// It supports both entities.User (JWT strategy) and auth.UserClaims (OIDC strategy).
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
	case pkgauth.UserClaims:
		return u.Roles, nil
	case *pkgauth.UserClaims:
		return u.Roles, nil
	default:
		return nil, pkgauth.ErrUnauthorized
	}
}
