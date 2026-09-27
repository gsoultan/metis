package entities

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestTheRolesAnAccountActsWithInAnOrganization(t *testing.T) {
	acme, globex := uuid.New(), uuid.New()
	account := User{
		Roles:               []string{RoleOperator},
		RolesByOrganization: map[uuid.UUID][]string{acme: {RoleAdmin}, uuid.Nil: {RoleDesigner}},
	}
	cases := []struct {
		name         string
		organization uuid.UUID
		want         []string
	}{
		{"where it holds a role of its own", acme, []string{RoleOperator, RoleAdmin}},
		{"where it holds none", globex, []string{RoleOperator}},
		// Whatever a map built by hand holds under the nil id, no organization
		// is no organization.
		{"no organization at all", uuid.Nil, []string{RoleOperator}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := account.RolesIn(tc.organization); !slices.Equal(got, tc.want) {
				t.Fatalf("acts with %v, want %v", got, tc.want)
			}
			for _, role := range []string{RoleOperator, RoleAdmin, RoleDesigner} {
				if got, want := account.HoldsRoleIn(tc.organization, role), slices.Contains(tc.want, role); got != want {
					t.Errorf("holds %s: %v, want %v", role, got, want)
				}
			}
		})
	}
	if account.HoldsRoleGlobally(RoleAdmin) {
		t.Error("a role held in one organization reads as held in every one")
	}
	if !account.HoldsRoleGlobally("operator") {
		t.Error("a global role is not recognised whatever its case")
	}
}

// Asking what somebody holds must not change it: the account a request carries
// is shared by every request that account makes while it is cached.
func TestRolesInLeavesTheAccountAsItWas(t *testing.T) {
	acme := uuid.New()
	global := make([]string, 1, 4)
	global[0] = RoleOperator
	account := User{Roles: global, RolesByOrganization: map[uuid.UUID][]string{acme: {RoleAdmin}}}

	_ = account.RolesIn(acme)
	if got := account.Roles[:cap(account.Roles)][1]; got != "" {
		t.Fatalf("working out the roles in Acme wrote %q into the global roles' spare capacity", got)
	}
}

func TestTheOrganizationARequestIsFor(t *testing.T) {
	acme := uuid.New()
	cases := []struct {
		name string
		ctx  context.Context
		want uuid.UUID
	}{
		{"a resolved organization", WithTenantContext(context.Background(), TenantContext{TenantID: acme.String()}), acme},
		{"none", context.Background(), uuid.Nil},
		{"an unreadable one", WithTenantContext(context.Background(), TenantContext{TenantID: "acme"}), uuid.Nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ActingOrganization(tc.ctx); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}
