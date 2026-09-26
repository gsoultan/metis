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
)

// organizationCount is an installation of a fixed number of organizations, or
// one that cannot say how many it has.
type organizationCount struct {
	n   int64
	err error
}

func (c organizationCount) CountOrganizations(context.Context) (int64, error) { return c.n, c.err }

func signedIn(principal any) context.Context {
	return context.WithValue(context.Background(), pkgauth.UserContextKey, principal)
}

func TestThePlatformAdministratorGate(t *testing.T) {
	named := uuid.New()
	other := uuid.New()
	errUnreachable := errors.New("database unreachable")

	cases := []struct {
		name          string
		organizations organizationCount
		caller        context.Context
		// admitted, or else the refusal wanted and what it must say.
		admitted bool
		want     error
		says     []string
	}{
		{
			name:          "a named administrator of a shared installation",
			organizations: organizationCount{n: 3},
			caller:        signedIn(entities.User{ID: named}),
			admitted:      true,
		},
		{
			name:          "named, carried by pointer",
			organizations: organizationCount{n: 3},
			caller:        signedIn(&entities.User{ID: named}),
			admitted:      true,
		},
		{
			name:          "an administrator of a single-organization installation",
			organizations: organizationCount{n: 1},
			caller:        signedIn(entities.User{ID: other}),
			admitted:      true,
		},
		{
			name:          "an administrator the operator did not name, on a shared installation",
			organizations: organizationCount{n: 2},
			caller:        signedIn(entities.User{ID: other}),
			want:          apierr.ErrForbidden,
			says:          []string{PlatformAdministratorsEnv, other.String()},
		},
		{
			// An identity provider's sign-in reaches here as the account linked
			// to it, which the operator can name like any other. Anything that is
			// still not an account cannot be named, whatever it claims.
			name:          "a principal that is not an account, on a shared installation",
			organizations: organizationCount{n: 2},
			caller:        signedIn(entities.IdentityClaims{Issuer: "https://idp.example", Subject: named.String()}),
			want:          apierr.ErrForbidden,
			says:          []string{PlatformAdministratorsEnv, "a local account's id"},
		},
		{
			name:          "an installation that cannot say whether it is shared",
			organizations: organizationCount{err: errUnreachable},
			caller:        signedIn(entities.User{ID: other}),
			want:          errUnreachable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(PlatformAdministratorsEnv, named.String())
			reached := false
			guarded := NewRequirePlatformAdministrator(tc.organizations).Intercept(func(context.Context, any) (any, error) {
				reached = true
				return "ok", nil
			})

			_, err := guarded(tc.caller, nil)
			if tc.admitted {
				if err != nil || !reached {
					t.Fatalf("refused (%v), want admitted", err)
				}
				return
			}
			if reached {
				t.Fatal("the endpoint ran for a caller the gate should have refused")
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			for _, part := range tc.says {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("the refusal does not say %q: %v", part, err)
				}
			}
		})
	}
}

// The operator's list names accounts by id. Anything else in it names nobody,
// and the nil id in particular must not name every caller without a local
// account, whose id reads as nil.
func TestOnlyAccountIDsNamePlatformAdministrators(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	got := platformAdministrators(" " + first.String() + ",,alice, " + uuid.Nil.String() + ",\t" + second.String() + " ")

	if len(got) != 2 {
		t.Fatalf("named %d accounts, want the two ids: %v", len(got), got)
	}
	for _, id := range []uuid.UUID{first, second} {
		if _, ok := got[id]; !ok {
			t.Errorf("%s is not named", id)
		}
	}
}
