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
		{"oidc preferred username", pkgauth.UserClaims{Subject: "sub-1", Username: "carol"}, "carol"},
		{"oidc subject only", pkgauth.UserClaims{Subject: "sub-2"}, "sub-2"},
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
		context.WithValue(context.Background(), pkgauth.UserContextKey, pkgauth.UserClaims{}),
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
		{"an identity provider's token", "Admin", pkgauth.UserClaims{Subject: "sub-1", Roles: []string{"Admin"}}},
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
