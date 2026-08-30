package tenant

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
)

// reached records whether the wrapped endpoint ran, and with what tenant.
type reached struct {
	called bool
	tenant entities.TenantContext
	hasCtx bool
}

func endpointRecording(r *reached) func(context.Context, any) (any, error) {
	return func(ctx context.Context, _ any) (any, error) {
		r.called = true
		r.tenant, r.hasCtx = entities.TenantContextFrom(ctx)
		return nil, nil
	}
}

func withPrincipal(t *testing.T, principal any) context.Context {
	t.Helper()
	return context.WithValue(t.Context(), pkgauth.UserContextKey, principal)
}

// TestOIDCPrincipalCannotReachTheDatabaseUnscoped is the regression guard for a
// live cross-tenant read.
//
// OIDC token validation returns *auth.UserClaims, which carries roles but no
// organization membership. organizationsFromContext could not resolve it, and
// the resolver used to treat that exactly like an unauthenticated request: it
// passed the call through with no TenantContext. The repository layer reads a
// missing TenantContext as a system call and applies no scoping at all, so an
// OIDC-authenticated user reached every tenant's rows, for reads and writes.
//
// An authenticated principal that cannot be resolved to a tenant must be
// refused, not waved through.
func TestOIDCPrincipalCannotReachTheDatabaseUnscoped(t *testing.T) {
	var got reached
	handler := NewEndpointTenantResolver().Intercept(endpointRecording(&got))

	ctx := withPrincipal(t, &pkgauth.UserClaims{
		Subject:  "oidc-subject",
		Username: "someone",
		Roles:    []string{"admin"},
	})

	_, err := handler(ctx, nil)

	if err == nil {
		t.Fatal("an OIDC principal with no resolvable organization was allowed through")
	}
	if !errors.Is(err, ErrUnresolvedTenant) {
		t.Errorf("err = %v, want %v", err, ErrUnresolvedTenant)
	}
	if got.called {
		t.Fatal("the endpoint ran anyway, so the repository layer would have seen every tenant")
	}
}

// TestUnauthenticatedRequestPassesThrough keeps public endpoints working. Login
// and setup have no principal by design, and denying them would lock everyone
// out of a fresh installation.
func TestUnauthenticatedRequestPassesThrough(t *testing.T) {
	var got reached
	handler := NewEndpointTenantResolver().Intercept(endpointRecording(&got))

	if _, err := handler(t.Context(), nil); err != nil {
		t.Fatalf("an unauthenticated request was refused: %v", err)
	}
	if !got.called {
		t.Fatal("a public endpoint did not run")
	}
	if got.hasCtx {
		t.Error("a request with no principal should carry no tenant")
	}
}

func TestResolverSelectsTenant(t *testing.T) {
	orgA, orgB := uuid.New(), uuid.New()

	user := entities.User{Organizations: []*entities.Organization{
		{ID: orgA}, {ID: orgB},
	}}

	tests := []struct {
		name      string
		principal any
		requested string
		wantErr   error
		wantAtive string
	}{
		{
			name:      "single membership needs no choice",
			principal: entities.User{Organizations: []*entities.Organization{{ID: orgA}}},
			wantAtive: orgA.String(),
		},
		{
			name:      "explicit choice among memberships is honoured",
			principal: user,
			requested: orgB.String(),
			wantAtive: orgB.String(),
		},
		{
			name:      "pointer principal resolves the same way",
			principal: &user,
			requested: orgB.String(),
			wantAtive: orgB.String(),
		},
		{
			name:      "an organization the caller is not in is refused",
			principal: user,
			requested: uuid.New().String(),
			wantErr:   ErrNotAMember,
		},
		{
			name:      "a principal with no membership is refused",
			principal: entities.User{},
			wantErr:   ErrNoTenantMembership,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got reached
			handler := NewEndpointTenantResolver().Intercept(endpointRecording(&got))

			ctx := withPrincipal(t, tc.principal)
			if tc.requested != "" {
				ctx = context.WithValue(ctx, organizationRequestKey{}, tc.requested)
			}

			_, err := handler(ctx, nil)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if got.called {
					t.Error("the endpoint ran despite the refusal")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.hasCtx {
				t.Fatal("no tenant was injected")
			}
			if got.tenant.TenantID != tc.wantAtive {
				t.Errorf("tenant = %q, want %q", got.tenant.TenantID, tc.wantAtive)
			}
		})
	}
}
