package interceptors

import (
	"context"
	"time"

	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/gobpm/internal/pkg/auth"
	"github.com/gsoultan/gobpm/server/domains/services"
	authinterceptor "github.com/gsoultan/gobpm/server/interceptors/auth"
	"github.com/gsoultan/gobpm/server/interceptors/contracts"
	"github.com/gsoultan/gobpm/server/interceptors/logging"
	"github.com/gsoultan/gobpm/server/interceptors/security"
	"github.com/gsoultan/gobpm/server/interceptors/tenant"
)

// InterceptorFactory creates various interceptors.
type InterceptorFactory struct {
	svc services.ServiceFacade
}

func NewInterceptorFactory(svc services.ServiceFacade) *InterceptorFactory {
	return &InterceptorFactory{svc: svc}
}

func (f *InterceptorFactory) NewLogging(method string) contracts.EndpointInterceptor {
	return logging.NewLoggingInterceptor(method)
}

func (f *InterceptorFactory) NewEndpointAuth() contracts.EndpointInterceptor {
	return authinterceptor.NewEndpointAuthInterceptor()
}

func (f *InterceptorFactory) NewHTTPAuth(strategy authinterceptor.SecurityStrategy) contracts.TransportInterceptor {
	return authinterceptor.NewHTTPAuthInterceptor(strategy)
}

func (f *InterceptorFactory) NewMandatoryHTTPAuth(strategy authinterceptor.SecurityStrategy, publicPaths []string) contracts.TransportInterceptor {
	return authinterceptor.NewMandatoryHTTPAuthInterceptor(strategy, publicPaths)
}

func (f *InterceptorFactory) NewRequestSize(maxBodyBytes int64) contracts.TransportInterceptor {
	return security.NewRequestSizeInterceptor(maxBodyBytes)
}

func (f *InterceptorFactory) NewRateLimit(maxRequests int, window time.Duration) contracts.TransportInterceptor {
	return security.NewRateLimitInterceptor(maxRequests, window)
}

func (f *InterceptorFactory) NewBackpressure(maxInFlightRequests, maxQueuedRequests int) contracts.TransportInterceptor {
	return security.NewBackpressureInterceptor(maxInFlightRequests, maxQueuedRequests)
}

func (f *InterceptorFactory) NewIdempotency(ttl time.Duration) contracts.TransportInterceptor {
	return security.NewIdempotencyInterceptor(ttl)
}

func (f *InterceptorFactory) NewJWTStrategy() authinterceptor.SecurityStrategy {
	return authinterceptor.NewJWTStrategy(f.svc.ValidateToken)
}

func (f *InterceptorFactory) NewOIDCStrategy(validator *auth.TokenValidator) authinterceptor.SecurityStrategy {
	return authinterceptor.NewOIDCStrategy(func(ctx context.Context, token string) (any, error) {
		return validator.ValidateToken(ctx, token)
	})
}

// NewTenantResolver derives the active tenant from the authenticated principal.
func (f *InterceptorFactory) NewTenantResolver() contracts.EndpointInterceptor {
	return tenant.NewEndpointTenantResolver()
}

// ProtectedChain applies logging, authentication and tenant resolution.
//
// Tenant resolution runs inside authentication so it can read the principal
// that auth put in the context; repositories then scope their queries to the
// resulting TenantContext.
func (f *InterceptorFactory) ProtectedChain(method string) func(endpoint.Endpoint) endpoint.Endpoint {
	logging := f.NewLogging(method)
	auth := f.NewEndpointAuth()
	tenantResolver := f.NewTenantResolver()
	return func(e endpoint.Endpoint) endpoint.Endpoint {
		return auth.Intercept(tenantResolver.Intercept(logging.Intercept(e)))
	}
}

// ProtectedChainWithRoles applies logging, authentication and role-based
// authorization to an endpoint.
//
// ProtectedChain only proves the caller is signed in. Endpoints that mutate
// tenant-wide state — users, groups, organizations, connectors, deployments —
// need to prove *who* is signed in, which is what this adds. Passing no roles
// is equivalent to ProtectedChain and should be reserved for endpoints where
// any authenticated participant is legitimately allowed.
func (f *InterceptorFactory) ProtectedChainWithRoles(method string, roles ...string) func(endpoint.Endpoint) endpoint.Endpoint {
	logging := f.NewLogging(method)
	auth := f.NewEndpointAuth()
	rbac := authinterceptor.NewRequireRoles(roles...)
	tenantResolver := f.NewTenantResolver()
	return func(e endpoint.Endpoint) endpoint.Endpoint {
		// Order matters: authenticate, then authorize, then scope. A missing
		// token reports "unauthenticated" rather than "insufficient role".
		return auth.Intercept(rbac.Intercept(tenantResolver.Intercept(logging.Intercept(e))))
	}
}

// PublicChain returns a function that applies only logging to an endpoint.
func (f *InterceptorFactory) PublicChain(method string) func(endpoint.Endpoint) endpoint.Endpoint {
	logging := f.NewLogging(method)
	return func(e endpoint.Endpoint) endpoint.Endpoint {
		return logging.Intercept(e)
	}
}
