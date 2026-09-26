package interceptors

import (
	"context"
	"sync"
	"time"

	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	authinterceptor "github.com/gsoultan/metis/server/interceptors/auth"
	"github.com/gsoultan/metis/server/interceptors/contracts"
	"github.com/gsoultan/metis/server/interceptors/logging"
	"github.com/gsoultan/metis/server/interceptors/security"
	"github.com/gsoultan/metis/server/interceptors/tenant"
	stormdb "github.com/gsoultan/metis/server/repositories/db"
)

// InterceptorFactory creates various interceptors.
type InterceptorFactory struct {
	users servicecontracts.UserService
	// platform is built on first use and shared, so the operator's list of
	// platform administrators is read — and a mistyped entry reported — once
	// per set of endpoints rather than once per endpoint.
	platform func() contracts.EndpointInterceptor
	// roles is every role-gated chain this factory has built, so what each
	// role is required for can be read from the gates. See RoleAccess.
	roles *roleRegistry
}

func NewInterceptorFactory(users servicecontracts.UserService, organizations servicecontracts.OrganizationCounter) *InterceptorFactory {
	return &InterceptorFactory{
		users: users,
		platform: sync.OnceValue(func() contracts.EndpointInterceptor {
			return authinterceptor.NewRequirePlatformAdministrator(organizations)
		}),
		roles: newRoleRegistry(),
	}
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

// NewIdempotency keeps idempotency records in the serving process.
//
// Correct for one replica, which is the supported topology. More than one wants
// NewIdempotencyOver, or a client retry landing on a different replica finds an
// empty cache and executes the write a second time.
func (f *InterceptorFactory) NewIdempotency(ttl time.Duration) contracts.TransportInterceptor {
	return security.NewIdempotencyInterceptor(ttl)
}

// NewIdempotencyOver keeps idempotency records in the database, so every
// replica gives the same answer to "has this already been done?".
func (f *InterceptorFactory) NewIdempotencyOver(conn *stormdb.Conn, ttl time.Duration) contracts.TransportInterceptor {
	if conn == nil {
		return f.NewIdempotency(ttl)
	}
	return security.NewIdempotencyInterceptorWithStore(security.NewDBIdempotencyStore(conn, ttl), ttl)
}

func (f *InterceptorFactory) NewJWTStrategy() authinterceptor.SecurityStrategy {
	return authinterceptor.NewJWTStrategy(f.users.ValidateToken)
}

// NewOIDCStrategy authenticates an identity provider's ID token and signs its
// holder in as the account linked to it.
//
// The token proves who somebody is to the provider. Which account that is here,
// and which organizations it may act in, is the sign-in's to decide — so the
// principal a request carries is that account, never the token's claims. That
// is what lets every check downstream treat an OIDC user as the account it is:
// roles an administrator granted, organizations the tenant resolver can place.
func (f *InterceptorFactory) NewOIDCStrategy(validator *auth.TokenValidator) authinterceptor.SecurityStrategy {
	return authinterceptor.NewOIDCStrategy(func(ctx context.Context, token string) (any, error) {
		claims, err := validator.ValidateToken(ctx, token)
		if err != nil {
			return nil, err
		}
		return f.users.SignInThroughIdentityProvider(ctx, claims)
	})
}

// NewLocalAndOIDCStrategy is what the API authenticates with while OIDC is on:
// a local account's token by the local rules, an ID token by the provider's
// and the sign-in above, each chosen by what the token says it is. Local
// accounts keep working then — the break-glass administrator included — when
// the provider is unreachable.
func (f *InterceptorFactory) NewLocalAndOIDCStrategy(validator *auth.TokenValidator) authinterceptor.SecurityStrategy {
	return authinterceptor.NewTokenKindStrategy(f.NewJWTStrategy(), f.NewOIDCStrategy(validator))
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
	f.roles.record(method, roles)
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

// RoleAccess lists each role with the actions its gates were built for, by
// the method names the chains were given.
//
// Read it once every chain is built: a chain built afterwards is enforced and
// missing from the answer, which is why MakeEndpoints reads it last.
func (f *InterceptorFactory) RoleAccess() []entities.RoleAccess {
	return f.roles.access()
}

// PlatformChain is for what every organization on the installation shares, where
// administering one of them is not enough: ProtectedChainWithRoles for
// administrators, then the platform administrator gate.
//
// Roles are global, so the administrator role alone admits the administrator
// of any organization — to a change every other organization then runs. The
// role it requires is recorded like any other gate's, so the legend lists
// these methods under the administrator; the platform gate after it is the
// operator's list, which no role grants.
func (f *InterceptorFactory) PlatformChain(method string) func(endpoint.Endpoint) endpoint.Endpoint {
	f.roles.record(method, []string{entities.RoleAdmin})
	logging := f.NewLogging(method)
	auth := f.NewEndpointAuth()
	rbac := authinterceptor.NewRequireRoles(entities.RoleAdmin)
	platform := f.platform()
	tenantResolver := f.NewTenantResolver()
	return func(e endpoint.Endpoint) endpoint.Endpoint {
		// The role first: a caller without it is told to ask for the role,
		// not about platform administrators.
		return auth.Intercept(rbac.Intercept(platform.Intercept(tenantResolver.Intercept(logging.Intercept(e)))))
	}
}

// PublicChain returns a function that applies only logging to an endpoint.
func (f *InterceptorFactory) PublicChain(method string) func(endpoint.Endpoint) endpoint.Endpoint {
	logging := f.NewLogging(method)
	return func(e endpoint.Endpoint) endpoint.Endpoint {
		return logging.Intercept(e)
	}
}
