package endpoints

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"

	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/endpoints/role"
)

// GET /api/v1/roles tells anybody signed in what each role is required for. It
// is read from the gates as the factory builds them; these tests hold what is
// served to what this file wires. A legend that could say "a designer may do
// X" while the gate refuses a designer X would be worse than the one sentence
// per role it replaces.

// roleConstants resolves the role constants endpoints.go names.
var roleConstants = map[string]string{
	"RoleAdmin":       entities.RoleAdmin,
	"RoleDesigner":    entities.RoleDesigner,
	"RoleOperator":    entities.RoleOperator,
	"RoleQueryAuthor": entities.RoleQueryAuthor,
	"RoleUser":        entities.RoleUser,
}

func TestTheRoleLegendIsWhatTheChainsEnforce(t *testing.T) {
	file := parseEndpointsFile(t)
	wired := gatedMethods(t, file, gatedChains(t, file))
	if len(wired) == 0 {
		t.Fatal("found no role-gated wiring in endpoints.go; this test no longer reads it")
	}
	served := servedLegend(t)

	for method, roles := range wired {
		if got := served[method]; !slices.Equal(got, roles) {
			t.Errorf("%s is gated on %v and the legend lists it under %v", method, roles, got)
		}
	}
	for method, roles := range served {
		if _, ok := wired[method]; !ok {
			t.Errorf("the legend lists %s under %v, which endpoints.go does not gate on a role", method, roles)
		}
	}
}

// Every adminOnly("X") is listed under the administrator and nobody else; the
// check above covers it, and this says it in the words the wiring uses.
func TestEveryAdminOnlyMethodIsListedUnderTheAdministratorAlone(t *testing.T) {
	file := parseEndpointsFile(t)
	served := servedLegend(t)
	for method, roles := range gatedMethods(t, file, gatedChains(t, file)) {
		if slices.Equal(roles, []string{entities.RoleAdmin}) &&
			!slices.Equal(served[method], []string{entities.RoleAdmin}) {
			t.Errorf("adminOnly(%q) is listed under %v", method, served[method])
		}
	}
}

// A gated action the legend cannot place in an area would be shown under
// nothing. The places are a table in entities/role_action_area.go; a new gate
// whose name it does not know fails here, naming the gate.
func TestEveryGatedActionIsPlacedInAnArea(t *testing.T) {
	for _, access := range listRoles(t).Roles {
		for _, action := range access.Actions {
			if action.Area == "" {
				t.Errorf("%s has no area; add the noun its name carries to entities/role_action_area.go", action.Method)
			}
			if action.Label == "" {
				t.Errorf("%s has no words", action.Method)
			}
		}
	}
}

// Who may read it: anybody signed in, and nobody else. What each role is for is
// the same for every caller — the installation's own gates, with nothing from
// any organization in it — so an account with no role at all is answered, and
// a caller with no identity is not.
func TestTheRoleLegendIsServedToAnybodySignedInAndNobodyElse(t *testing.T) {
	if reply := listRoles(t); len(reply.Roles) == 0 {
		t.Fatal("an account with no role was given an empty legend")
	}

	eps := MakeEndpoints(nil)
	if _, err := eps.Role.ListRoles(context.Background(), nil); !errors.Is(err, pkgauth.ErrUnauthorized) {
		t.Fatalf("a caller with no identity: got %v, want ErrUnauthorized", err)
	}
}

func parseEndpointsFile(t *testing.T) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "endpoints.go", nil, 0)
	if err != nil {
		t.Fatalf("parse endpoints.go: %v", err)
	}
	return file
}

// gatedChains maps each chain helper that requires a role to the roles it
// requires, read from its definition:
//
//	designer := func(method string) ... {
//		return f.ProtectedChainWithRoles(method, entities.RoleAdmin, entities.RoleDesigner)
//	}
func gatedChains(t *testing.T, file *ast.File) map[string][]string {
	t.Helper()
	chains := map[string][]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		name, isIdent := assign.Lhs[0].(*ast.Ident)
		helper, isFunc := assign.Rhs[0].(*ast.FuncLit)
		if !isIdent || !isFunc {
			return true
		}
		if roles := rolesRequiredBy(t, helper); roles != nil {
			chains[name.Name] = roles
		}
		return true
	})
	if len(chains) == 0 {
		t.Fatal("found no chain helper calling ProtectedChainWithRoles in endpoints.go")
	}
	return chains
}

// rolesRequiredBy is the roles a helper passes to ProtectedChainWithRoles, in
// sorted order, or nil when it does not call it.
func rolesRequiredBy(t *testing.T, helper *ast.FuncLit) []string {
	t.Helper()
	var roles []string
	ast.Inspect(helper.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fun, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || fun.Sel.Name != "ProtectedChainWithRoles" {
			return true
		}
		for _, arg := range call.Args[1:] {
			selector, ok := arg.(*ast.SelectorExpr)
			if !ok {
				t.Fatalf("a chain helper passes %T as a role; this test reads entities.RoleX only", arg)
			}
			value, known := roleConstants[selector.Sel.Name]
			if !known {
				t.Fatalf("a chain helper requires %s, which this test does not know", selector.Sel.Name)
			}
			roles = append(roles, value)
		}
		return false
	})
	slices.Sort(roles)
	return roles
}

// gatedMethods maps every method wired through a role-gated helper —
// `adminOnly("CreateUser")(...)` — to the roles that helper requires.
func gatedMethods(t *testing.T, file *ast.File, chains map[string][]string) map[string][]string {
	t.Helper()
	methods := map[string][]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		helper, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		roles, gated := chains[helper.Name]
		literal, isString := call.Args[0].(*ast.BasicLit)
		if !gated || !isString || literal.Kind != token.STRING {
			return true
		}
		method, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatalf("unquote %s: %v", literal.Value, err)
		}
		if earlier, twice := methods[method]; twice && !slices.Equal(earlier, roles) {
			t.Errorf("%s is wired twice, on %v and on %v; the legend can list it only once", method, earlier, roles)
		}
		methods[method] = roles
		return true
	})
	return methods
}

// servedLegend is what GET /api/v1/roles answers, as method → roles, sorted.
func servedLegend(t *testing.T) map[string][]string {
	t.Helper()
	served := map[string][]string{}
	for _, access := range listRoles(t).Roles {
		for _, action := range access.Actions {
			served[action.Method] = append(served[action.Method], access.Role)
		}
	}
	for method := range served {
		slices.Sort(served[method])
	}
	return served
}

// listRoles asks the served endpoint, through its chain, as an account with no
// role at all.
func listRoles(t *testing.T) role.ListRolesResponse {
	t.Helper()
	eps := MakeEndpoints(nil)
	reply, err := eps.Role.ListRoles(signedIn(), nil)
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	response, ok := reply.(role.ListRolesResponse)
	if !ok {
		t.Fatalf("list roles answered %T", reply)
	}
	if response.Err != nil {
		t.Fatalf("list roles: %v", response.Err)
	}
	return response
}

// signedIn is a request from an account with no role at all, in an
// organization — what the auth interceptor hands a protected endpoint.
func signedIn() context.Context {
	return context.WithValue(context.Background(), pkgauth.UserContextKey, entities.User{
		ID:            uuid.Must(uuid.NewV7()),
		Username:      "nobody-special",
		Organizations: []*entities.Organization{{ID: uuid.Must(uuid.NewV7())}},
	})
}
