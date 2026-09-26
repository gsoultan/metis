package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
)

func okEndpoint(_ context.Context, _ any) (any, error) { return "ok", nil }

func ctxWithEntityUser(roles []string) context.Context {
	u := entities.User{ID: uuid.New(), Roles: roles}
	return context.WithValue(context.Background(), pkgauth.UserContextKey, u)
}

// ctxWithLinkedAccount is somebody signed in through an identity provider: the
// OIDC strategy leaves the account linked to them, with the roles an
// administrator granted it.
func ctxWithLinkedAccount(roles []string) context.Context {
	u := entities.User{ID: uuid.New(), Roles: roles, IdentityProvider: "https://id.example.com"}
	return context.WithValue(context.Background(), pkgauth.UserContextKey, u)
}

func TestRBACInterceptor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		ctx           context.Context
		requiredRoles []string
		policy        AccessPolicy
		wantErr       bool
		// want is the kind of refusal: ErrUnauthorized when nobody is known,
		// ErrForbidden when somebody is and lacks the right.
		want error
	}{
		{
			name:          "no user in context",
			ctx:           context.Background(),
			requiredRoles: []string{"admin"},
			policy:        NewAllowAllPolicy(),
			wantErr:       true,
			want:          pkgauth.ErrUnauthorized,
		},
		{
			name:          "user has required role (entities.User)",
			ctx:           ctxWithEntityUser([]string{"admin", "editor"}),
			requiredRoles: []string{"admin"},
			policy:        NewAllowAllPolicy(),
			wantErr:       false,
		},
		{
			name:          "user has required role (account signed in through an identity provider)",
			ctx:           ctxWithLinkedAccount([]string{"admin"}),
			requiredRoles: []string{"admin"},
			policy:        NewAllowAllPolicy(),
			wantErr:       false,
		},
		{
			// Roles are the account's, granted by an administrator. A token's
			// claims are never the caller, whatever they carry.
			name: "a token's claims are nobody",
			ctx: context.WithValue(context.Background(), pkgauth.UserContextKey,
				entities.IdentityClaims{Issuer: "https://id.example.com", Subject: "sub-1"}),
			requiredRoles: []string{"admin"},
			policy:        NewAllowAllPolicy(),
			wantErr:       true,
			want:          pkgauth.ErrUnauthorized,
		},
		{
			name:          "user missing required role",
			ctx:           ctxWithEntityUser([]string{"viewer"}),
			requiredRoles: []string{"admin"},
			policy:        NewAllowAllPolicy(),
			wantErr:       true,
			want:          apierr.ErrForbidden,
		},
		{
			name:          "one of multiple required roles matches",
			ctx:           ctxWithEntityUser([]string{"editor"}),
			requiredRoles: []string{"admin", "editor"},
			policy:        NewAllowAllPolicy(),
			wantErr:       false,
		},
		{
			name:          "empty required roles allows any authenticated user",
			ctx:           ctxWithEntityUser([]string{}),
			requiredRoles: []string{},
			policy:        NewAllowAllPolicy(),
			wantErr:       false,
		},
		{
			name:          "policy denies even with correct role",
			ctx:           ctxWithEntityUser([]string{"admin"}),
			requiredRoles: []string{"admin"},
			policy:        &denyAllPolicy{},
			wantErr:       true,
			want:          apierr.ErrForbidden,
		},
		{
			name:          "unrecognised context value type",
			ctx:           context.WithValue(context.Background(), pkgauth.UserContextKey, "not-a-user"),
			requiredRoles: []string{"admin"},
			policy:        NewAllowAllPolicy(),
			wantErr:       true,
			want:          pkgauth.ErrUnauthorized,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			interceptor := NewRBACInterceptor(tc.requiredRoles, tc.policy, "action", "resource")
			wrapped := interceptor.Intercept(okEndpoint)
			_, err := wrapped(tc.ctx, nil)
			if tc.wantErr && err == nil {
				t.Errorf("expected error but got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error but got: %v", err)
			}
			if tc.wantErr && err != nil && !errors.Is(err, tc.want) {
				t.Errorf("expected %v, got: %v", tc.want, err)
			}
		})
	}
}

func TestNewRequireRoles_PassesNextOnMatch(t *testing.T) {
	t.Parallel()
	interceptor := NewRequireRoles("admin")
	wrapped := interceptor.Intercept(okEndpoint)
	result, err := wrapped(ctxWithEntityUser([]string{"admin"}), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %v", result)
	}
}

func TestAllowAllPolicy(t *testing.T) {
	t.Parallel()
	p := NewAllowAllPolicy()
	if !p.Allow(context.Background(), []string{"any"}, "any", "any") {
		t.Error("AllowAll policy should always return true")
	}
}

// denyAllPolicy is a test-only AccessPolicy that always denies.
type denyAllPolicy struct{}

func (*denyAllPolicy) Allow(_ context.Context, _ []string, _, _ string) bool { return false }
