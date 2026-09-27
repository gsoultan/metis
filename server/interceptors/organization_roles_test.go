package interceptors_test

import (
	"context"
	"errors"
	"testing"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/interceptors"
	"github.com/gsoultan/metis/server/interceptors/tenant"
)

// A role an account holds in one organization acts in a request for that
// organization and in no other request, whichever chain the endpoint is on.
// Every chain is asked the same five questions: nobody's role, a role held only
// in the other organization, a role held in the organization the request is
// for, a global role, and a caller with no role at all on the chain that asks
// for none.

// installation is a fixed number of organizations, for the platform gate.
type installation int64

func (n installation) CountOrganizations(context.Context) (int64, error) { return int64(n), nil }

// reachedIn records whether the endpoint ran, and for which organization.
type reachedIn struct {
	ran    bool
	tenant string
}

func (r *reachedIn) endpoint() endpoint.Endpoint {
	return func(ctx context.Context, _ any) (any, error) {
		r.ran = true
		if tc, ok := entities.TenantContextFrom(ctx); ok {
			r.tenant = tc.TenantID
		}
		return "ok", nil
	}
}

// memberOfBoth is somebody in Acme and in Globex, holding global in both and
// here in Acme alone.
func memberOfBoth(acme, globex uuid.UUID, global []string, here ...string) entities.User {
	user := entities.User{
		ID:            uuid.Must(uuid.NewV7()),
		Username:      "kim",
		Roles:         global,
		Organizations: []*entities.Organization{{ID: acme}, {ID: globex}},
	}
	if len(here) > 0 {
		user.RolesByOrganization = map[uuid.UUID][]string{acme: here}
	}
	return user
}

// askingFor is a request from caller for one of its organizations, as the
// transport leaves it: the principal, and the organization it chose.
func askingFor(caller entities.User, organization uuid.UUID) context.Context {
	ctx := context.WithValue(context.Background(), pkgauth.UserContextKey, caller)
	return tenant.WithRequestedOrganization(ctx, organization.String())
}

type chainCase struct {
	name    string
	caller  entities.User
	askFor  uuid.UUID
	admit   bool
	refusal error
}

func runChain(t *testing.T, chain func(endpoint.Endpoint) endpoint.Endpoint, cases []chainCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got reachedIn
			_, err := chain(got.endpoint())(askingFor(tc.caller, tc.askFor), nil)
			if !tc.admit {
				if !errors.Is(err, tc.refusal) {
					t.Fatalf("got %v, want %v", err, tc.refusal)
				}
				if got.ran {
					t.Fatal("the endpoint ran for a caller the chain should have refused")
				}
				return
			}
			if err != nil || !got.ran {
				t.Fatalf("refused (%v), want admitted", err)
			}
			if got.tenant != tc.askFor.String() {
				t.Fatalf("the endpoint ran for organization %q, want the one asked for, %s", got.tenant, tc.askFor)
			}
		})
	}
}

func TestProtectedChainWithRolesCountsTheRolesHeldWhereTheRequestIs(t *testing.T) {
	acme, globex := uuid.New(), uuid.New()
	chain := interceptors.NewInterceptorFactory(nil, installation(2)).
		ProtectedChainWithRoles("DeleteGroup", entities.RoleAdmin)

	runChain(t, chain, []chainCase{
		{"no role at all", memberOfBoth(acme, globex, nil), acme, false, apierr.ErrForbidden},
		{"the role only in the other organization", memberOfBoth(acme, globex, nil, entities.RoleAdmin), globex, false, apierr.ErrForbidden},
		{"the role in the organization asked for", memberOfBoth(acme, globex, nil, entities.RoleAdmin), acme, true, nil},
		{"the role in every organization, asking for one", memberOfBoth(acme, globex, []string{entities.RoleAdmin}), acme, true, nil},
		{"the role in every organization, asking for the other", memberOfBoth(acme, globex, []string{entities.RoleAdmin}), globex, true, nil},
		{"another role where the request is", memberOfBoth(acme, globex, nil, entities.RoleDesigner), acme, false, apierr.ErrForbidden},
	})
}

// The organization the role is looked up in is the one the resolver settled
// on. Asking for an organization the caller is not in is refused as not a
// member before any role is looked at — never answered with the roles of the
// organization it is in.
func TestProtectedChainWithRolesRefusesAnOrganizationTheCallerIsNotIn(t *testing.T) {
	acme, globex, initech := uuid.New(), uuid.New(), uuid.New()
	chain := interceptors.NewInterceptorFactory(nil, installation(3)).
		ProtectedChainWithRoles("DeleteGroup", entities.RoleAdmin)

	runChain(t, chain, []chainCase{
		{"an administrator of Acme asking for Initech", memberOfBoth(acme, globex, nil, entities.RoleAdmin), initech, false, pkgauth.ErrUnauthorized},
	})
}

func TestProtectedChainWithGlobalRolesCountsOnlyTheRolesHeldEverywhere(t *testing.T) {
	acme, globex := uuid.New(), uuid.New()
	chain := interceptors.NewInterceptorFactory(nil, installation(2)).
		ProtectedChainWithGlobalRoles("CreateOrganization", entities.RoleAdmin)

	runChain(t, chain, []chainCase{
		{"no role at all", memberOfBoth(acme, globex, nil), acme, false, apierr.ErrForbidden},
		{"the role only in the other organization", memberOfBoth(acme, globex, nil, entities.RoleAdmin), globex, false, apierr.ErrForbidden},
		{"the role in the organization asked for", memberOfBoth(acme, globex, nil, entities.RoleAdmin), acme, false, apierr.ErrForbidden},
		{"the role in every organization", memberOfBoth(acme, globex, []string{entities.RoleAdmin}), acme, true, nil},
	})
}

// On an installation of one organization the platform gate admits every
// administrator — every administrator of every organization, which there is
// one of. A role held in that organization alone still does not count.
func TestPlatformChainCountsOnlyTheRolesHeldEverywhere(t *testing.T) {
	t.Setenv("METIS_PLATFORM_ADMINS", "")
	acme := uuid.New()
	only := func(global []string, here ...string) entities.User {
		user := entities.User{ID: uuid.Must(uuid.NewV7()), Username: "kim", Roles: global,
			Organizations: []*entities.Organization{{ID: acme}}}
		if len(here) > 0 {
			user.RolesByOrganization = map[uuid.UUID][]string{acme: here}
		}
		return user
	}
	chain := interceptors.NewInterceptorFactory(nil, installation(1)).PlatformChain("InstallConnectorManifest")

	runChain(t, chain, []chainCase{
		{"no role at all", only(nil), acme, false, apierr.ErrForbidden},
		{"the role in the only organization", only(nil, entities.RoleAdmin), acme, false, apierr.ErrForbidden},
		{"the role in every organization", only([]string{entities.RoleAdmin}), acme, true, nil},
	})
}

// A chain that asks for no role asks for none held anywhere: signing in, in an
// organization, is enough — with or without roles, wherever they are held.
func TestProtectedChainAsksForNoRole(t *testing.T) {
	acme, globex := uuid.New(), uuid.New()
	chain := interceptors.NewInterceptorFactory(nil, installation(2)).ProtectedChain("ListTasks")

	runChain(t, chain, []chainCase{
		{"no role at all", memberOfBoth(acme, globex, nil), globex, true, nil},
		{"a role only in the other organization", memberOfBoth(acme, globex, nil, entities.RoleAdmin), globex, true, nil},
	})
}
