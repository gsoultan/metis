package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/interceptors/contracts"
)

// endpointAuthInterceptor verifies the JWT token from context (extracted in transport).
type endpointAuthInterceptor struct{}

func NewEndpointAuthInterceptor() contracts.EndpointInterceptor {
	return &endpointAuthInterceptor{}
}

func (i *endpointAuthInterceptor) Intercept(next endpoint.Endpoint) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		user := ctx.Value(auth.UserContextKey)
		if user == nil {
			return nil, auth.ErrUnauthorized
		}
		return next(ctx, request)
	}
}

// httpAuthInterceptor extracts the user from the Authorization header and puts it in the context.
type httpAuthInterceptor struct {
	strategy SecurityStrategy
}

func NewHTTPAuthInterceptor(strategy SecurityStrategy) contracts.TransportInterceptor {
	return &httpAuthInterceptor{strategy: strategy}
}

func (i *httpAuthInterceptor) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			next.ServeHTTP(w, r)
			return
		}

		token, ok := bearerTokenFromHeader(authHeader)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		u, err := i.strategy.Authenticate(r.Context(), token)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), auth.UserContextKey, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// mandatoryHTTPAuthInterceptor is like httpAuthInterceptor but returns 401 on failure.
type mandatoryHTTPAuthInterceptor struct {
	strategy      SecurityStrategy
	publicPathSet map[string]struct{}
}

// publicPathPrefixes are the API paths that are public along with everything
// beneath them.
//
// Kept here rather than accepted as a parameter, and deliberately short: an
// exact-match list cannot go wrong, whereas a prefix that someone adds casually
// can open a whole subtree. Every entry must be justified in a comment and must
// end in a slash — without the slash, "/api/v1/hooks" would also make
// "/api/v1/hooksecrets" public.
var publicPathPrefixes = []string{
	// Webhook deliveries. A partner's webhook configuration screen has nowhere
	// to put a bearer token this engine would recognise; the token in the path
	// identifies the webhook and an HMAC signature over the body authenticates
	// it. Everything beneath this prefix is a single handler that verifies that
	// signature before it looks at anything.
	"/api/v1/hooks/",
}

func NewMandatoryHTTPAuthInterceptor(strategy SecurityStrategy, publicPaths []string) contracts.TransportInterceptor {
	publicPathSet := make(map[string]struct{}, len(publicPaths))
	for _, publicPath := range publicPaths {
		publicPathSet[publicPath] = struct{}{}
	}

	return &mandatoryHTTPAuthInterceptor{
		strategy:      strategy,
		publicPathSet: publicPathSet,
	}
}

func (i *mandatoryHTTPAuthInterceptor) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only protect /api/ endpoints.
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		// Exclude public API endpoints
		if i.isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		token, ok := bearerTokenFromHeader(authHeader)
		if !ok {
			http.Error(w, "Invalid auth header", http.StatusUnauthorized)
			return
		}

		u, err := i.strategy.Authenticate(r.Context(), token)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), auth.UserContextKey, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (i *mandatoryHTTPAuthInterceptor) isPublicPath(path string) bool {
	if _, ok := i.publicPathSet[path]; ok {
		return true
	}
	for _, prefix := range publicPathPrefixes {
		// A bare prefix match is not enough: the path must go *beneath* it, so
		// that "/api/v1/hooks/" opening up does not also open "/api/v1/hooks"
		// itself or anything that merely starts with those characters.
		if strings.HasPrefix(path, prefix) && len(path) > len(prefix) {
			return true
		}
	}
	return false
}

func bearerTokenFromHeader(authHeader string) (string, bool) {
	prefix, token, found := strings.Cut(authHeader, " ")
	if !found || prefix != "Bearer" || token == "" || strings.ContainsAny(token, " \t") {
		return "", false
	}

	return token, true
}
