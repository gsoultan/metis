package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
)

func okEndpoint(_ context.Context, _ any) (any, error) { return "ok", nil }

func ctxWithEntityUser(roles []string) context.Context {
	u := entities.User{ID: uuid.New(), Roles: roles}
	return context.WithValue(context.Background(), pkgauth.UserContextKey, u)
}

func ctxWithClaimsUser(roles []string) context.Context {
	c := pkgauth.UserClaims{Roles: roles}
	return context.WithValue(context.Background(), pkgauth.UserContextKey, c)
}

func TestRBACInterceptor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		ctx           context.Context
		requiredRoles []string
		policy        AccessPolicy
		wantErr       bool
	}{
		{
			name:          "no user in context",
			ctx:           context.Background(),
			requiredRoles: []string{"admin"},
			policy:        NewAllowAllPolicy(),
			wantErr:       true,
		},
		{
			name:          "user has required role (entities.User)",
			ctx:           ctxWithEntityUser([]string{"admin", "editor"}),
			requiredRoles: []string{"admin"},
			policy:        NewAllowAllPolicy(),
			wantErr:       false,
		},
		{
			name:          "user has required role (UserClaims)",
			ctx:           ctxWithClaimsUser([]string{"admin"}),
			requiredRoles: []string{"admin"},
			policy:        NewAllowAllPolicy(),
			wantErr:       false,
		},
		{
			name:          "user missing required role",
			ctx:           ctxWithEntityUser([]string{"viewer"}),
			requiredRoles: []string{"admin"},
			policy:        NewAllowAllPolicy(),
			wantErr:       true,
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
		},
		{
			name:          "unrecognised context value type",
			ctx:           context.WithValue(context.Background(), pkgauth.UserContextKey, "not-a-user"),
			requiredRoles: []string{"admin"},
			policy:        NewAllowAllPolicy(),
			wantErr:       true,
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
			if tc.wantErr && err != nil && !errors.Is(err, pkgauth.ErrUnauthorized) {
				t.Errorf("expected ErrUnauthorized, got: %v", err)
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
