package tenant

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/interceptors/contracts"
)

// ErrMissingTenant is returned when a request arrives without a resolvable tenant.
var ErrMissingTenant = errors.New("tenant: missing or unresolvable tenant context")

// TenantResolver extracts a TenantContext from an incoming HTTP request.
// Implementations may read from JWT claims, a custom header, or subdomain.
type TenantResolver interface {
	ResolveFromRequest(r *http.Request) (entities.TenantContext, error)
}

// httpTenantInterceptor is a TransportInterceptor that resolves the tenant from
// the HTTP request and injects TenantContext into the request context.
type httpTenantInterceptor struct {
	resolver TenantResolver
}

// NewHTTPTenantInterceptor creates a transport-level tenant injector.
func NewHTTPTenantInterceptor(resolver TenantResolver) contracts.TransportInterceptor {
	return &httpTenantInterceptor{resolver: resolver}
}

func (i *httpTenantInterceptor) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tc, err := i.resolver.ResolveFromRequest(r)
		if err != nil {
			// Missing tenant is not fatal at transport level — let endpoint guard enforce it.
			next.ServeHTTP(w, r.WithContext(r.Context()))
			return
		}
		ctx := entities.WithTenantContext(r.Context(), tc)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// endpointTenantGuard is an EndpointInterceptor that rejects requests without
// a valid TenantContext in the context, enforcing tenant isolation at the domain boundary.
type endpointTenantGuard struct{}

// NewEndpointTenantGuard creates an endpoint-level tenant validator.
func NewEndpointTenantGuard() contracts.EndpointInterceptor {
	return &endpointTenantGuard{}
}

func (g *endpointTenantGuard) Intercept(next endpoint.Endpoint) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		tc, ok := entities.TenantContextFrom(ctx)
		if !ok || tc.TenantID == "" {
			return nil, ErrMissingTenant
		}
		return next(ctx, request)
	}
}
