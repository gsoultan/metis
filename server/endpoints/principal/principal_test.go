package principal

import (
	"context"
	"errors"
	"testing"

	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
)

func TestUsernameComesFromTheVerifiedPrincipalOnly(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"local user", entities.User{Username: "alice", Roles: []string{entities.RoleUser}}, "alice"},
		{"local user pointer", &entities.User{Username: "bob"}, "bob"},
		{"an account signed in through an identity provider",
			entities.User{Username: "carol", IdentityProvider: "https://id.example.com"}, "carol"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), pkgauth.UserContextKey, c.value)
			got, err := Username(ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestUsernameRefusesAnonymousAndEmptyPrincipals(t *testing.T) {
	for _, ctx := range []context.Context{
		context.Background(),
		context.WithValue(context.Background(), pkgauth.UserContextKey, entities.User{}),
		context.WithValue(context.Background(), pkgauth.UserContextKey, (*entities.User)(nil)),
		// A token's claims are evidence for a sign-in, never the caller: the
		// OIDC strategy resolves them to an account before any endpoint runs.
		context.WithValue(context.Background(), pkgauth.UserContextKey,
			entities.IdentityClaims{Issuer: "https://id.example.com", Subject: "sub-1", Username: "carol"}),
	} {
		if _, err := Username(ctx); !errors.Is(err, pkgauth.ErrUnauthorized) {
			t.Fatalf("expected ErrUnauthorized, got %v", err)
		}
	}
}

func TestHasRoleDeniesWhenAbsent(t *testing.T) {
	if HasRole(context.Background(), entities.RoleAdmin) {
		t.Fatal("anonymous caller reported as admin")
	}
	ctx := context.WithValue(context.Background(), pkgauth.UserContextKey, entities.User{Username: "u", Roles: []string{entities.RoleUser}})
	if HasRole(ctx, entities.RoleAdmin) {
		t.Fatal("USER reported as ADMIN")
	}
	if !HasRole(ctx, entities.RoleUser) {
		t.Fatal("USER not reported as USER")
	}
	// Roles are what an administrator granted the account. A token's claims in
	// the context are nobody, whatever they say.
	claims := context.WithValue(context.Background(), pkgauth.UserContextKey,
		entities.IdentityClaims{Issuer: "https://id.example.com", Subject: "sub-1"})
	if HasRole(claims, entities.RoleAdmin) {
		t.Fatal("a token's claims were reported as holding a role")
	}
}

// Every administrator-only endpoint checks the role ignoring case, and accounts
// written by the older role picker hold "admin". HasRole compared exactly, so
// the same administrator passed an endpoint's gate and was then refused the
// administrator's override inside it.
func TestHasRoleIgnoresCaseAsTheEndpointGatesDo(t *testing.T) {
	cases := []struct {
		name      string
		held      string
		principal any
	}{
		{"a local account", "admin", entities.User{Username: "olga", Roles: []string{"admin"}}},
		{"an account signed in through an identity provider", "Admin",
			entities.User{Username: "olga-sso", Roles: []string{"Admin"}, IdentityProvider: "https://id.example.com"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.WithValue(t.Context(), pkgauth.UserContextKey, c.principal)
			if !HasRole(ctx, entities.RoleAdmin) {
				t.Fatalf("%s holding %q is not reported as %s", c.name, c.held, entities.RoleAdmin)
			}
		})
	}
}
