package interceptors_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/interceptors"
)

// What a role allows was one hand-written sentence per role, held to nothing.
// The factory now writes it down as it builds each gate, which is the one
// moment the method and the roles it requires are both in hand.

// methodsFor is the methods the legend lists under role, in its order.
func methodsFor(access []entities.RoleAccess, role string) []string {
	for _, entry := range access {
		if entry.Role != role {
			continue
		}
		methods := []string{}
		for _, action := range entry.Actions {
			methods = append(methods, action.Method)
		}
		return methods
	}
	return nil
}

func rolesListed(access []entities.RoleAccess) []string {
	var roles []string
	for _, entry := range access {
		roles = append(roles, entry.Role)
	}
	return roles
}

func TestTheFactoryRecordsWhichRolesEachGateRequires(t *testing.T) {
	f := interceptors.NewInterceptorFactory(nil)
	f.ProtectedChainWithRoles("DeleteUser", entities.RoleAdmin)
	f.ProtectedChainWithRoles("CreateDefinition", entities.RoleAdmin, entities.RoleDesigner)
	f.ProtectedChainWithRoles("ResolveIncident", entities.RoleAdmin, entities.RoleOperator)
	f.ProtectedChainWithRoles("CreateUser", entities.RoleAdmin)
	// Neither of these gates on a role, so neither is anybody's to list.
	f.ProtectedChain("ListUsers")
	f.ProtectedChainWithRoles("GetUser")

	access := f.RoleAccess()

	cases := map[string][]string{
		entities.RoleAdmin:    {"CreateDefinition", "ResolveIncident", "CreateUser", "DeleteUser"},
		entities.RoleDesigner: {"CreateDefinition"},
		entities.RoleOperator: {"ResolveIncident"},
		// Checked where a process is deployed, not by a gate: listed, with nothing.
		entities.RoleQueryAuthor: {},
	}
	for role, want := range cases {
		if got := methodsFor(access, role); !slices.Equal(got, want) {
			t.Errorf("%s is listed as required for %v, want %v", role, got, want)
		}
	}
}

// Every built-in role is listed, in its own order, whether or not a gate names
// it — "required for nothing here" is an answer, and a missing role is not.
func TestEveryBuiltInRoleIsListedInOrder(t *testing.T) {
	access := interceptors.NewInterceptorFactory(nil).RoleAccess()

	want := []string{entities.RoleAdmin, entities.RoleDesigner, entities.RoleOperator, entities.RoleQueryAuthor}
	if got := rolesListed(access); !slices.Equal(got, want) {
		t.Fatalf("listed %v, want %v", got, want)
	}

	encoded, err := json.Marshal(access)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(string(encoded), "null") {
		t.Fatalf("a role with no actions encodes as null rather than []: %s", encoded)
	}
}

// A gate that names a role nobody built in is still a gate. Leaving it out of
// the legend would hide exactly what the legend is for.
func TestARoleOnlyAGateNamesIsListedAfterTheBuiltInOnes(t *testing.T) {
	f := interceptors.NewInterceptorFactory(nil)
	f.ProtectedChainWithRoles("ExportAuditTrail", "AUDITOR", entities.RoleAdmin)

	access := f.RoleAccess()
	roles := rolesListed(access)
	if roles[len(roles)-1] != "AUDITOR" {
		t.Fatalf("listed %v; the gate's AUDITOR role is missing or out of place", roles)
	}
	if got := methodsFor(access, "AUDITOR"); !slices.Equal(got, []string{"ExportAuditTrail"}) {
		t.Fatalf("AUDITOR is listed as required for %v", got)
	}
}

// Built once at startup and served to everybody, so it must not depend on the
// order the gates happened to be built in.
func TestTheLegendDoesNotDependOnTheOrderTheGatesWereBuilt(t *testing.T) {
	gates := []struct {
		method string
		roles  []string
	}{
		{"DeleteGroup", []string{entities.RoleAdmin}},
		{"UpdateDecision", []string{entities.RoleAdmin, entities.RoleDesigner}},
		{"BroadcastSignal", []string{entities.RoleAdmin, entities.RoleOperator}},
		{"CreateGroup", []string{entities.RoleAdmin}},
		{"CreateDecision", []string{entities.RoleAdmin, entities.RoleDesigner}},
	}
	forwards := interceptors.NewInterceptorFactory(nil)
	for _, gate := range gates {
		forwards.ProtectedChainWithRoles(gate.method, gate.roles...)
	}
	backwards := interceptors.NewInterceptorFactory(nil)
	for _, gate := range slices.Backward(gates) {
		backwards.ProtectedChainWithRoles(gate.method, gate.roles...)
	}

	if a, b := forwards.RoleAccess(), backwards.RoleAccess(); !reflect.DeepEqual(a, b) {
		t.Fatalf("the same gates built in another order give another legend:\n%+v\n%+v", a, b)
	}
}
