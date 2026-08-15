package tenant

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-kit/kit/endpoint"
	pkgauth "github.com/gsoultan/gobpm/internal/pkg/auth"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/interceptors/contracts"
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
		orgs, ok := organizationsFromContext(ctx)
		if !ok {
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
// principal belongs to. The second result is false when the request carries no
// authenticated principal at all.
func organizationsFromContext(ctx context.Context) ([]string, bool) {
	v := ctx.Value(pkgauth.UserContextKey)
	if v == nil {
		return nil, false
	}

	var user *entities.User
	switch u := v.(type) {
	case entities.User:
		user = &u
	case *entities.User:
		user = u
	default:
		// OIDC claims carry no membership list; the OIDC deployment path must
		// map claims to organizations before this can scope anything.
		return nil, false
	}

	ids := make([]string, 0, len(user.Organizations))
	for _, o := range user.Organizations {
		if o != nil {
			ids = append(ids, o.ID.String())
		}
	}
	return ids, true
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
