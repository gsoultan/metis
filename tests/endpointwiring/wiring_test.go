// Package endpointwiring_test asserts that every endpoint the application
// declares is put through an authorization chain.
package endpointwiring_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Declaring an endpoint and wiring it are two steps, and the second is silent
// when it is missed.
//
// `MakeEndpoints` builds a struct of endpoints; `endpoints.go` then replaces
// each field with a wrapped copy that resolves the tenant and checks the role.
// A field nobody wraps is still routed and still served — it simply arrives at
// the repository with no identity and no role check. Nothing fails, nothing
// logs, and the endpoint works well enough to look correct.
//
// Four were found that way, by hand, after an unrelated soak happened to notice
// one of them:
//
//   - ExportOCEL returned 200 and an empty log under a strict tenant scope.
//   - CreateConnector, UpdateConnector and DeleteConnector let any authenticated
//     account add or rewrite a connector — what the engine calls out to, and
//     with which credentials — while creating an *instance* of one needed an
//     administrator.
//   - UpdateDecision sat between CreateDecision and DeleteDecision, both gated
//     on the designer role, and was gated on nothing. The same account was
//     refused a create with 401 and allowed an update with 200.
//
// So this reads the source rather than trusting a reviewer to notice. It is a
// static check on purpose: the wrapping is a compile-time fact, and a runtime
// assertion would need every endpoint exercised to find the one that is missing.
func TestEveryDeclaredEndpointIsWrapped(t *testing.T) {
	root := repoRoot(t)

	declared := declaredEndpoints(t, filepath.Join(root, "server", "endpoints"))
	if len(declared) == 0 {
		t.Fatal("no endpoint structs found; this test is looking in the wrong place")
	}

	wrapped := wrappedEndpoints(t, filepath.Join(root, "server", "endpoints", "endpoints.go"))
	if len(wrapped) == 0 {
		t.Fatal("no wrapped endpoints found; this test is looking in the wrong place")
	}

	var missing []string
	for name, pkg := range declared {
		if _, ok := wrapped[name]; !ok {
			missing = append(missing, pkg+"."+name)
		}
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Errorf("%d endpoint(s) are declared but never put through an authorization chain.\n"+
			"Each one is routed and served with no tenant resolved and no role checked.\n"+
			"Wire them in server/endpoints/endpoints.go:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// declaredEndpoints maps every endpoint.Endpoint field name to its package.
//
// Keyed by field name rather than by package because endpoints.go names its
// variables freely — externalTaskEndpoints, sourceEndpoints, accountEndpoints —
// so matching on the package would report a wired endpoint as missing. Field
// names are unique across the tree; the check below fails loudly if that stops
// being true, because a collision would let one wrapped field vouch for another.
func declaredEndpoints(t *testing.T, dir string) map[string]string {
	t.Helper()
	found := map[string]string{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name(), "endpoint.go")
		if _, err := os.Stat(path); err != nil {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok || spec.Name.Name != "Endpoints" {
				return true
			}
			structType, ok := spec.Type.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range structType.Fields.List {
				if !isEndpointType(field.Type) {
					continue
				}
				for _, name := range field.Names {
					if other, clash := found[name.Name]; clash {
						t.Fatalf("endpoint %q is declared in both %s and %s; "+
							"this test matches on field name and cannot tell them apart",
							name.Name, other, entry.Name())
					}
					found[name.Name] = entry.Name()
				}
			}
			return true
		})
	}
	return found
}

// isEndpointType reports whether a field is declared as endpoint.Endpoint.
func isEndpointType(expr ast.Expr) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "endpoint" && selector.Sel.Name == "Endpoint"
}

// wrappedEndpoints is every field assigned from one of the authorization chains.
//
// It reads assignments of the shape `xEndpoints.Field = chain("Name")(...)`,
// which is how every wired endpoint is written.
func wrappedEndpoints(t *testing.T, path string) map[string]struct{} {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	chains := map[string]bool{
		"protected": true, "adminOnly": true, "designer": true,
		"operator": true, "public": true,
	}

	wrapped := map[string]struct{}{}
	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range assign.Lhs {
			target, ok := lhs.(*ast.SelectorExpr)
			if !ok || i >= len(assign.Rhs) {
				continue
			}
			if chainOf(assign.Rhs[i], chains) {
				wrapped[target.Sel.Name] = struct{}{}
			}
		}
		return true
	})
	return wrapped
}

// chainOf reports whether an expression is a call to one of the chain helpers,
// i.e. `chain("Name")(endpoint)`.
func chainOf(expr ast.Expr, chains map[string]bool) bool {
	outer, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	inner, ok := outer.Fun.(*ast.CallExpr)
	if !ok {
		return false
	}
	name, ok := inner.Fun.(*ast.Ident)
	return ok && chains[name.Name]
}

// repoRoot walks up until it finds go.mod, so the test does not depend on where
// `go test` was invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the test's working directory")
		}
		dir = parent
	}
}
