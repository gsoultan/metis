package endpoints

import (
	"context"
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	authinterceptor "github.com/gsoultan/metis/server/interceptors/auth"
)

// The RBAC interceptor was written, unit-tested and then referenced nowhere:
// the middleware chain authenticated but never authorized, so any signed-in
// user could manage users, groups, organizations and connector credentials.
// These tests assert the wiring, not the interceptor — a passing rbac_test.go
// told us nothing about whether the chain was actually applied.

func ctxWithRoles(roles ...string) context.Context {
	return context.WithValue(context.Background(), pkgauth.UserContextKey, entities.User{
		Username: "tester",
		Roles:    roles,
	})
}

func reachedEndpoint() (endpoint.Endpoint, *bool) {
	reached := false
	return func(context.Context, any) (any, error) {
		reached = true
		return "ok", nil
	}, &reached
}

func TestRequireRoles_DeniesCallerWithoutTheRole(t *testing.T) {
	ep, reached := reachedEndpoint()
	guarded := authinterceptor.NewRequireRoles(entities.RoleAdmin).Intercept(ep)

	// Forbidden, not unauthorized: the caller is known and lacks the right.
	_, err := guarded(ctxWithRoles(entities.RoleUser), nil)
	if !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("non-admin reached an admin endpoint: got %v, want ErrForbidden", err)
	}
	if *reached {
		t.Fatal("endpoint body executed despite the role check failing")
	}
}

func TestRequireRoles_DeniesUnauthenticatedCaller(t *testing.T) {
	ep, reached := reachedEndpoint()
	guarded := authinterceptor.NewRequireRoles(entities.RoleAdmin).Intercept(ep)

	_, err := guarded(context.Background(), nil)
	if !errors.Is(err, pkgauth.ErrUnauthorized) {
		t.Fatalf("anonymous caller: got %v, want ErrUnauthorized", err)
	}
	if *reached {
		t.Fatal("endpoint body executed for an anonymous caller")
	}
}

func TestRequireRoles_AllowsHolderOfAnyRequiredRole(t *testing.T) {
	ep, reached := reachedEndpoint()
	guarded := authinterceptor.NewRequireRoles(entities.RoleAdmin, entities.RoleDesigner).Intercept(ep)

	if _, err := guarded(ctxWithRoles(entities.RoleDesigner), nil); err != nil {
		t.Fatalf("designer denied on a designer endpoint: %v", err)
	}
	if !*reached {
		t.Fatal("endpoint body did not execute for an authorized caller")
	}
}

// Setup seeds "ADMIN"; a token minted elsewhere may carry "admin". A
// case-sensitive comparison would deny a legitimate administrator, which
// presents as a broken login rather than a policy decision.
func TestRequireRoles_MatchesRolesCaseInsensitively(t *testing.T) {
	ep, _ := reachedEndpoint()
	guarded := authinterceptor.NewRequireRoles(entities.RoleAdmin).Intercept(ep)

	if _, err := guarded(ctxWithRoles(strings.ToLower(entities.RoleAdmin)), nil); err != nil {
		t.Fatalf(`role "admin" denied against required "ADMIN": %v`, err)
	}
}

// The point of this file: prove the chain is applied to the endpoints that
// need it, so the interceptor cannot silently become dead code again.
func TestMakeEndpoints_AdministrativeEndpointsAreRoleGated(t *testing.T) {
	source := readEndpointsSource(t)

	adminGated := []string{
		"CreateGroup", "UpdateGroup", "DeleteGroup",
		"AddMembership", "RemoveMembership",
		"CreateUser", "UpdateUser", "DeleteUser",
		"CreateOrganization", "UpdateOrganization", "DeleteOrganization",
		"CreateProject", "UpdateProject", "DeleteProject",
		"CreateConnectorInstance", "UpdateConnectorInstance", "DeleteConnectorInstance",
	}
	for _, name := range adminGated {
		if !strings.Contains(source, `adminOnly("`+name+`")`) {
			t.Errorf("%s is not admin-gated; a compromised ordinary account could call it", name)
		}
	}

	// Authoring endpoints let a caller make the engine execute code.
	for _, name := range []string{"CreateDefinition", "DeleteDefinition", "ImportDefinition", "ExecuteScript"} {
		if !strings.Contains(source, `designer("`+name+`")`) {
			t.Errorf("%s is not restricted to designers", name)
		}
	}

	// Nothing administrative may remain on the plain authenticated chain.
	for _, name := range adminGated {
		if strings.Contains(source, `protected("`+name+`")`) {
			t.Errorf("%s is still on the plain authenticated chain", name)
		}
	}

	// Nor on the public chain, which is worse: PublicChain applies logging and
	// nothing else, so an endpoint there gets no role check *and* no tenant
	// resolution. CreateUser sat here, which let any authenticated caller post a
	// user carrying roles:["admin"] and somebody else's organization.
	for _, name := range adminGated {
		if strings.Contains(source, `public("`+name+`")`) {
			t.Errorf("%s is on the public chain: no role check and no tenant scoping", name)
		}
	}

	// The only endpoints that may be public are the ones reachable before a
	// caller can possibly hold a token.
	for _, name := range []string{"Login", "GetSetupStatus", "Setup", "TestConnection"} {
		if !strings.Contains(source, `public("`+name+`")`) {
			t.Errorf("%s must stay public or a fresh installation cannot be reached", name)
		}
	}
}

// Connector manifests and connector templates are installation-wide, so
// adminOnly — which admits the administrator of any one organization — is not
// enough to change them.
func TestMakeEndpoints_InstallationWideEndpointsNeedAPlatformAdministrator(t *testing.T) {
	source := readEndpointsSource(t)
	for _, name := range []string{
		"InstallConnectorManifest", "SetConnectorManifestEnabled", "DeleteConnectorManifest",
		"CreateConnector", "UpdateConnector", "DeleteConnector",
	} {
		if !strings.Contains(source, `platformAdmin("`+name+`")`) {
			t.Errorf("%s is not behind the platform administrator gate; any organization's administrator could call it", name)
		}
	}
}

// publicEndpoints are the only ones reachable before a caller can hold a token.
var publicEndpoints = []string{"GetSetupStatus", "Login", "Setup", "TestConnection"}

// The assertion above checks that these four are public. It never checked that
// nothing else is, and CreateOrganization sat on the public chain beside them —
// logging and nothing else, reachable by any signed-in account over HTTP and
// by anybody at all over the gRPC listener, which applies no authentication.
func TestMakeEndpoints_NothingElseIsPublic(t *testing.T) {
	source := readEndpointsSource(t)

	var found []string
	for _, match := range regexp.MustCompile(`public\("([A-Za-z]+)"\)`).FindAllStringSubmatch(source, -1) {
		found = append(found, match[1])
	}
	slices.Sort(found)
	found = slices.Compact(found)

	if !slices.Equal(found, publicEndpoints) {
		t.Fatalf("the public chain carries %v; only %v may be reachable without a role check", found, publicEndpoints)
	}
}

// readEndpointsSource returns the wiring source so the test can assert which
// chain each endpoint is on. Reading the source is deliberate: the chains are
// closures with identical signatures, so there is nothing to introspect at
// runtime, and the property worth protecting is "the gate is applied here".
func readEndpointsSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("endpoints.go")
	if err != nil {
		t.Fatalf("read endpoints.go: %v", err)
	}
	return string(b)
}
