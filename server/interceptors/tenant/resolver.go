package tenant

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-kit/kit/endpoint"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/interceptors/contracts"
)

// OrganizationHeader lets a caller who belongs to several organizations choose
// which one a request applies to.
//
// It is a *selection*, never an assertion: the value must match one of the
// memberships on the authenticated principal, which are loaded from the
// database during token validation. A caller cannot reach an organization they
// are not a member of by setting this header.
const OrganizationHeader = "X-Organization-ID"

// ErrNoTenantMembership is returned when the caller belongs to no organization.
var ErrNoTenantMembership = fmt.Errorf("tenant: authenticated principal has no organization membership")

// ErrNotAMember is returned when the requested organization is not one the
// caller belongs to.
var ErrNotAMember = fmt.Errorf("tenant: %w", pkgauth.ErrUnauthorized)

// ErrUnresolvedTenant is returned when a request carries an authenticated
// principal that cannot be resolved to an organization.
//
// This case used to be indistinguishable from an unauthenticated request, and
// both were waved through. That was a cross-tenant read: OIDC token validation
// left the token's claims in the context, with roles but no membership list,
// so every OIDC-authenticated request arrived with no TenantContext — which the
// repository layer reads as a system call and does not scope at all.
//
// An identity provider's sign-in now reaches here as the account linked to it,
// placed in the organizations its claim names. Anything that is still not an
// account is refused: a principal whose tenant cannot be determined has no
// bounded view of the data, and serving it an unbounded one is the failure
// this error exists to prevent.
var ErrUnresolvedTenant = fmt.Errorf("tenant: %w: the authenticated principal could not be resolved to an organization",
	pkgauth.ErrUnauthorized)

// NewEndpointTenantResolver derives the active tenant from the *authenticated
// principal* and injects it into the context for repository scoping.
//
// This replaces HeaderTenantResolver, which read the tenant straight from a
// client-controlled header — any authenticated user could read another
// organization's data by changing one request header. Resolution now starts
// from the principal that token validation loaded from the database.
//
// It must run after authentication. Requests with no principal pass through
// untouched so that public endpoints (login, setup) keep working; the
// repository layer treats a missing TenantContext as "system call", and the
// endpoint auth interceptor is what stops an anonymous caller reaching a
// protected endpoint in the first place.
func NewEndpointTenantResolver() contracts.EndpointInterceptor {
	return &endpointTenantResolver{}
}

type endpointTenantResolver struct{}

func (r *endpointTenantResolver) Intercept(next endpoint.Endpoint) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		orgs, hasPrincipal, err := organizationsFromContext(ctx)
		if err != nil {
			// An authenticated principal we cannot place in an organization is
			// refused rather than passed through. Passing through leaves the
			// repository layer with no TenantContext, which it reads as a system
			// call and does not scope.
			return nil, err
		}
		if !hasPrincipal {
			// No principal at all: a public endpoint such as login or setup.
			// The auth interceptor is what keeps anonymous callers away from
			// protected endpoints; this resolver has nothing to scope by.
			return next(ctx, request)
		}
		if len(orgs) == 0 {
			return nil, ErrNoTenantMembership
		}

		requested := requestedOrganization(ctx)
		active, err := selectOrganization(orgs, requested)
		if err != nil {
			return nil, err
		}

		return next(entities.WithTenantContext(ctx, entities.TenantContext{TenantID: active}), request)
	}
}

// selectOrganization picks the tenant for this request, verifying any explicit
// choice against the caller's actual memberships.
func selectOrganization(memberships []string, requested string) (string, error) {
	if requested == "" {
		return memberships[0], nil
	}
	for _, id := range memberships {
		if id == requested {
			return id, nil
		}
	}
	return "", fmt.Errorf("%w: not a member of organization %s", ErrNotAMember, requested)
}

// organizationsFromContext returns the organization IDs the authenticated
// principal belongs to.
//
// The three outcomes must stay distinct, because collapsing the last two into
// the first is what let OIDC callers read every tenant:
//
//	(nil,  false, nil) — no principal; a public endpoint, nothing to scope by
//	(orgs, true,  nil) — resolved
//	(nil,  true,  err) — a principal we cannot place; refuse
func organizationsFromContext(ctx context.Context) ([]string, bool, error) {
	v := ctx.Value(pkgauth.UserContextKey)
	if v == nil {
		return nil, false, nil
	}

	var user *entities.User
	switch u := v.(type) {
	case entities.User:
		user = &u
	case *entities.User:
		user = u
	default:
		// Not an account. Both strategies leave one — an identity provider's
		// sign-in is resolved to the account linked to it, with the
		// organizations its claim names — so whatever this is carries no
		// memberships to scope by. Refused: the alternative is serving it
		// unscoped.
		return nil, true, fmt.Errorf("%w (principal type %T)", ErrUnresolvedTenant, v)
	}

	ids := make([]string, 0, len(user.Organizations))
	for _, o := range user.Organizations {
		if o != nil {
			ids = append(ids, o.ID.String())
		}
	}
	return ids, true, nil
}

type organizationRequestKey struct{}

// WithRequestedOrganization records the organization the caller selected via
// OrganizationHeader, for the endpoint resolver to validate.
func WithRequestedOrganization(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, organizationRequestKey{}, id)
}

func requestedOrganization(ctx context.Context) string {
	// No organisation on the context means none was requested.
	id, ok := ctx.Value(organizationRequestKey{}).(string)
	if !ok {
		return ""
	}
	return id
}

// NewHTTPOrganizationSelector carries OrganizationHeader from the request into
// the context so the endpoint resolver can validate it against the caller's
// memberships. It performs no authorization itself.
func NewHTTPOrganizationSelector() contracts.TransportInterceptor {
	return &httpOrganizationSelector{}
}

type httpOrganizationSelector struct{}

func (i *httpOrganizationSelector) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := r.Header.Get(OrganizationHeader); id != "" {
			r = r.WithContext(WithRequestedOrganization(r.Context(), id))
		}
		next.ServeHTTP(w, r)
	})
}

// ResolveFromContext derives the active tenant from an authenticated request
// context, outside the endpoint chain.
//
// The endpoint resolver covers everything that goes through go-kit. The event
// stream does not: it is a long-lived handler mounted straight on the mux, so
// without this it would see no tenant at all — and a stream with no tenant
// either delivers nothing or delivers everything, depending on which way the
// registry reads an empty scope. Both are wrong answers, and one of them is a
// disclosure.
//
// Resolution starts from the principal that token validation loaded, exactly as
// the endpoint resolver does. An explicit organization is still checked against
// the caller's memberships; ok is false when there is no principal or no
// membership to select.
func ResolveFromContext(ctx context.Context) (entities.TenantContext, bool) {
	orgs, hasPrincipal, err := organizationsFromContext(ctx)
	if err != nil || !hasPrincipal || len(orgs) == 0 {
		return entities.TenantContext{}, false
	}
	active, err := selectOrganization(orgs, requestedOrganization(ctx))
	if err != nil {
		return entities.TenantContext{}, false
	}
	return entities.TenantContext{TenantID: active}, true
}
