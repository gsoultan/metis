package endpoints

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/go-kit/kit/endpoint"
	pkgauth "github.com/gsoultan/gobpm/internal/pkg/auth"
	"github.com/gsoultan/gobpm/server/domains/entities"
	authinterceptor "github.com/gsoultan/gobpm/server/interceptors/auth"
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

	_, err := guarded(ctxWithRoles(entities.RoleUser), nil)
	if !errors.Is(err, pkgauth.ErrUnauthorized) {
		t.Fatalf("non-admin reached an admin endpoint: got %v, want ErrUnauthorized", err)
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
		"UpdateUser", "DeleteUser",
		"UpdateOrganization", "DeleteOrganization",
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
