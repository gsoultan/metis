package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/interceptors/contracts"
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

// The roles a request acts with are the account's global roles and the ones it
// holds in the organization the request is for — the tenant resolver's answer,
// which is on the context by the time the check runs. With no organization on
// it the global roles are all there is.
func TestTheRoleCheckCountsTheOrganizationTheRequestIsFor(t *testing.T) {
	t.Parallel()
	acme, globex := uuid.New(), uuid.New()
	designerInAcme := entities.User{ID: uuid.New(), RolesByOrganization: map[uuid.UUID][]string{acme: {"DESIGNER"}}}
	in := func(organization uuid.UUID, user any) context.Context {
		ctx := context.WithValue(context.Background(), pkgauth.UserContextKey, user)
		if organization == uuid.Nil {
			return ctx
		}
		return entities.WithTenantContext(ctx, entities.TenantContext{TenantID: organization.String()})
	}

	cases := []struct {
		name  string
		ctx   context.Context
		check func(...string) contracts.EndpointInterceptor
		want  error
	}{
		{"held in the organization the request is for", in(acme, designerInAcme), NewRequireRoles, nil},
		{"held by pointer", in(acme, &designerInAcme), NewRequireRoles, nil},
		{"held only in another organization", in(globex, designerInAcme), NewRequireRoles, apierr.ErrForbidden},
		{"a request for no organization", in(uuid.Nil, designerInAcme), NewRequireRoles, apierr.ErrForbidden},
		{"a gate for what every organization shares", in(acme, designerInAcme), NewRequireGlobalRoles, apierr.ErrForbidden},
		{"a global role at that gate", in(acme, entities.User{ID: uuid.New(), Roles: []string{"DESIGNER"}}), NewRequireGlobalRoles, nil},
		{"a nil account", in(acme, (*entities.User)(nil)), NewRequireRoles, pkgauth.ErrUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tc.check("DESIGNER").Intercept(okEndpoint)(tc.ctx, nil)
			if tc.want == nil && err != nil {
				t.Fatalf("refused: %v", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

// A refusal says where the role would have to be held, so the person refused
// knows whom to ask: an administrator of this organization, or nobody in any
// one organization at all.
func TestARoleRefusalSaysWhereTheRoleIsMissing(t *testing.T) {
	t.Parallel()
	acme := uuid.New()
	ctx := entities.WithTenantContext(ctxWithEntityUser(nil), entities.TenantContext{TenantID: acme.String()})

	_, here := NewRequireRoles("ADMIN").Intercept(okEndpoint)(ctx, nil)
	if here == nil || !strings.Contains(here.Error(), "does not hold in this organization") {
		t.Errorf("a refusal in an organization reads %v", here)
	}
	_, everywhere := NewRequireGlobalRoles("ADMIN").Intercept(okEndpoint)(ctx, nil)
	if everywhere == nil || !strings.Contains(everywhere.Error(), "in every organization") {
		t.Errorf("a refusal at a gate for what every organization shares reads %v", everywhere)
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
