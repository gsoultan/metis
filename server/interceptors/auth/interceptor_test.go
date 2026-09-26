package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
)

type strategyFunc func(ctx context.Context, token string) (any, error)

func (f strategyFunc) Authenticate(ctx context.Context, token string) (any, error) {
	return f(ctx, token)
}

func TestBearerTokenFromHeader(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		header   string
		expected string
		isValid  bool
	}{
		{
			name:     "valid bearer header",
			header:   "Bearer token-value",
			expected: "token-value",
			isValid:  true,
		},
		{
			name:    "empty header",
			header:  "",
			isValid: false,
		},
		{
			name:    "missing token",
			header:  "Bearer",
			isValid: false,
		},
		{
			name:    "invalid scheme",
			header:  "Token abc",
			isValid: false,
		},
		{
			name:    "extra spaces are invalid",
			header:  "Bearer  abc",
			isValid: false,
		},
	}

	for _, testCase := range testCases {

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			token, ok := bearerTokenFromHeader(testCase.header)
			if ok != testCase.isValid {
				t.Fatalf("expected validity %t, got %t", testCase.isValid, ok)
			}

			if token != testCase.expected {
				t.Fatalf("expected token %q, got %q", testCase.expected, token)
			}
		})
	}
}

func TestHTTPAuthInterceptor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                string
		authorizationHeader string
		strategy            SecurityStrategy
		expectNextCalled    bool
		expectUserInContext bool
	}{
		{
			name:             "allows request without auth header",
			strategy:         strategyFunc(func(context.Context, string) (any, error) { return nil, nil }),
			expectNextCalled: true,
		},
		{
			name:                "ignores invalid auth header",
			authorizationHeader: "Token abc",
			strategy:            strategyFunc(func(context.Context, string) (any, error) { return nil, nil }),
			expectNextCalled:    true,
		},
		{
			name:                "ignores failed authentication",
			authorizationHeader: "Bearer abc",
			strategy: strategyFunc(func(context.Context, string) (any, error) {
				return nil, errors.New("invalid token")
			}),
			expectNextCalled: true,
		},
		{
			name:                "injects user into context on successful auth",
			authorizationHeader: "Bearer abc",
			strategy: strategyFunc(func(context.Context, string) (any, error) {
				return "user-1", nil
			}),
			expectNextCalled:    true,
			expectUserInContext: true,
		},
	}

	for _, testCase := range testCases {

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			interceptor := NewHTTPAuthInterceptor(testCase.strategy)

			nextCalled := false
			handler := interceptor.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				if testCase.expectUserInContext {
					if r.Context().Value(pkgauth.UserContextKey) == nil {
						t.Fatalf("expected user in context")
					}
				} else if r.Context().Value(pkgauth.UserContextKey) != nil {
					t.Fatalf("did not expect user in context")
				}

				w.WriteHeader(http.StatusNoContent)
			}))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/setup/status", nil)
			if testCase.authorizationHeader != "" {
				req.Header.Set("Authorization", testCase.authorizationHeader)
			}

			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)

			if res.Code != http.StatusNoContent {
				t.Fatalf("expected status %d, got %d", http.StatusNoContent, res.Code)
			}

			if nextCalled != testCase.expectNextCalled {
				t.Fatalf("expected next called %t, got %t", testCase.expectNextCalled, nextCalled)
			}
		})
	}
}

func TestMandatoryHTTPAuthInterceptor(t *testing.T) {
	t.Parallel()

	const publicSetupPath = "/api/v1/setup/status"

	testCases := []struct {
		name                string
		requestPath         string
		authorizationHeader string
		strategy            SecurityStrategy
		expectedStatus      int
		expectNextCalled    bool
		expectUserInContext bool
	}{
		{
			name:             "allows non api path without auth",
			requestPath:      "/healthz",
			strategy:         strategyFunc(func(context.Context, string) (any, error) { return nil, nil }),
			expectedStatus:   http.StatusNoContent,
			expectNextCalled: true,
		},
		{
			name:             "allows configured public api path without auth",
			requestPath:      publicSetupPath,
			strategy:         strategyFunc(func(context.Context, string) (any, error) { return nil, nil }),
			expectedStatus:   http.StatusNoContent,
			expectNextCalled: true,
		},
		{
			name:           "rejects protected api path without auth",
			requestPath:    "/api/v1/tasks",
			strategy:       strategyFunc(func(context.Context, string) (any, error) { return nil, nil }),
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:                "rejects invalid auth header",
			requestPath:         "/api/v1/tasks",
			authorizationHeader: "Token abc",
			strategy:            strategyFunc(func(context.Context, string) (any, error) { return nil, nil }),
			expectedStatus:      http.StatusUnauthorized,
		},
		{
			name:                "rejects failed authentication",
			requestPath:         "/api/v1/tasks",
			authorizationHeader: "Bearer abc",
			strategy: strategyFunc(func(context.Context, string) (any, error) {
				return nil, errors.New("invalid token")
			}),
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:                "allows protected api path with valid auth and sets context user",
			requestPath:         "/api/v1/tasks",
			authorizationHeader: "Bearer abc",
			strategy: strategyFunc(func(context.Context, string) (any, error) {
				return "user-1", nil
			}),
			expectedStatus:      http.StatusNoContent,
			expectNextCalled:    true,
			expectUserInContext: true,
		},
	}

	for _, testCase := range testCases {

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			interceptor := NewMandatoryHTTPAuthInterceptor(testCase.strategy, []string{publicSetupPath})

			nextCalled := false
			handler := interceptor.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				if testCase.expectUserInContext && r.Context().Value(pkgauth.UserContextKey) == nil {
					t.Fatalf("expected user in context")
				}
				w.WriteHeader(http.StatusNoContent)
			}))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, testCase.requestPath, nil)
			if testCase.authorizationHeader != "" {
				req.Header.Set("Authorization", testCase.authorizationHeader)
			}

			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)

			if res.Code != testCase.expectedStatus {
				t.Fatalf("expected status %d, got %d", testCase.expectedStatus, res.Code)
			}

			if nextCalled != testCase.expectNextCalled {
				t.Fatalf("expected next called %t, got %t", testCase.expectNextCalled, nextCalled)
			}
		})
	}
}

// The webhook endpoint is public by design, and that exemption is a prefix
// rather than an exact path because the token is a path segment. A prefix is
// exactly the shape that opens more than intended if it is written carelessly,
// so what it does and does not cover is pinned here.
func TestPublicPathPrefixes(t *testing.T) {
	interceptor := NewMandatoryHTTPAuthInterceptor(strategyFunc(func(context.Context, string) (any, error) { return nil, nil }), nil)
	inner, ok := interceptor.(*mandatoryHTTPAuthInterceptor)
	if !ok {
		t.Fatalf("unexpected interceptor type %T", interceptor)
	}

	public := []string{
		"/api/v1/hooks/abc123",
		"/api/v1/hooks/a",
		"/api/v1/hooks/abc123/anything-beneath",
	}
	for _, path := range public {
		if !inner.isPublicPath(path) {
			t.Errorf("%q is not public; a webhook delivery would be refused before its signature was checked", path)
		}
	}

	// The dangerous half. Every one of these starts with the same characters as
	// the prefix and must still require a token.
	protected := []string{
		"/api/v1/hooks",         // the collection itself: listing webhooks is not public
		"/api/v1/hooks/",        // the bare prefix addresses no webhook
		"/api/v1/hooksecrets",   // merely starts with the same characters
		"/api/v1/hooks-admin/x", // ditto, with a plausible name
		"/api/v1/tasks",
		"/api/v1/definitions",
	}
	for _, path := range protected {
		if inner.isPublicPath(path) {
			t.Errorf("%q was treated as public", path)
		}
	}
}

// A token that proves nothing is a 401, and one that proves who somebody is,
// for somebody nothing admits, is a 403 carrying the reason — which is what
// tells them, and whoever they ask, what has to change.
func TestMandatoryAuthTellsNotAdmittedFromNotAuthenticated(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantReason string
	}{
		{"a token that proves nothing", errors.New("failed to verify token"), http.StatusUnauthorized, ""},
		{"somebody authenticated but not admitted",
			apierr.Forbiddenf("an operator has to set %s", pkgauth.EnvOrganizationClaim),
			http.StatusForbidden, pkgauth.EnvOrganizationClaim},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			refusing := strategyFunc(func(context.Context, string) (any, error) { return nil, tc.err })
			reached := false
			handler := NewMandatoryHTTPAuthInterceptor(refusing, nil).Wrap(
				http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/projects", nil)
			req.Header.Set("Authorization", "Bearer token-value")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if reached {
				t.Fatal("the request went on after its token was refused")
			}
			if rec.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantReason == "" {
				return
			}
			var reply struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &reply); err != nil || !strings.Contains(reply.Error, tc.wantReason) {
				t.Fatalf("the refusal %q does not carry its reason", rec.Body.String())
			}
		})
	}
}
