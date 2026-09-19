// Package maintenancemode_test asserts that a maintenance command does not
// start the engine.
//
// `metis --reset-password <user>` is a recovery command. It ran the full
// startup sequence first, because `setupService` — step 3 of App.Run — ended by
// calling StartWorkers, StartScheduledSyncs, startSSEFanout,
// startEnvironmentWorkers and startSharedLimits. The --reset-password branch
// returns at step 3b, *after* that.
//
// So the command started ten job workers against the shared database, claimed
// whatever was ready under a five-minute lease, printed the password and
// exited — leaving that work locked and unworked until the lease expired.
// Measured on a development database: a --reset-password run logged
// "Job worker started workers=10" and "SSE fan-out started" under its own
// replica id, alongside the already-running server's.
//
// An operator reaches for --reset-password when somebody is locked out, which
// is during an incident, which is the worst moment to stall the job queue.
//
// This is a static check for the same reason tests/endpointwiring is: the call
// site is a compile-time fact, and proving it at runtime would mean booting the
// app against a real database and observing an absence — which is the shape of
// assertion that passes for the wrong reason.
package maintenancemode_test

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

// Calls that start something which acts on its own, and therefore must not run
// during a maintenance command.
var backgroundStarters = map[string]string{
	"StartWorkers":            "job workers claim jobs under a lease",
	"StartScheduledSyncs":     "directory syncs run on a timer",
	"startSSEFanout":          "the event fan-out reads the broadcast table",
	"startEnvironmentWorkers": "per-environment job workers claim jobs too",
	"startSharedLimits":       "the shared rate-limit counter registers this replica",
}

func TestSetupServiceStartsNothingInTheBackground(t *testing.T) {
	fn := findMethod(t, "setupService")

	var found []string
	ast.Inspect(fn, func(n ast.Node) bool {
		if name, ok := calleeName(n); ok {
			if why, isStarter := backgroundStarters[name]; isStarter {
				found = append(found, name+" — "+why)
			}
		}
		return true
	})
	sort.Strings(found)

	if len(found) > 0 {
		t.Errorf("setupService starts %d background worker(s).\n"+
			"It runs at step 3 of App.Run, before the --reset-password branch returns at 3b,\n"+
			"so a password reset would start the engine and then exit, stranding any job it\n"+
			"claimed for the length of its lease.\n"+
			"Move these into startBackgroundWork, which Run calls only on the server path:\n  %s",
			len(found), strings.Join(found, "\n  "))
	}
}

// The other half of the same fact: the server path must still start them, or
// nothing would ever run and every process would hang at its first wait.
func TestRunStartsBackgroundWorkAfterTheMaintenanceBranch(t *testing.T) {
	fn := findMethod(t, "Run")

	var sawReset, sawStart bool
	var resetAt, startAt token.Pos

	ast.Inspect(fn, func(n ast.Node) bool {
		name, ok := calleeName(n)
		if !ok {
			return true
		}
		switch name {
		case "handleResetPassword":
			if !sawReset {
				sawReset, resetAt = true, n.Pos()
			}
		case "startBackgroundWork":
			if !sawStart {
				sawStart, startAt = true, n.Pos()
			}
		}
		return true
	})

	if !sawStart {
		t.Fatal("App.Run never calls startBackgroundWork, so no job worker, timer sweep or " +
			"event fan-out would ever run and every process would hang at its first wait")
	}
	if !sawReset {
		t.Fatal("App.Run never calls handleResetPassword, so --reset-password does nothing")
	}
	if startAt < resetAt {
		t.Error("App.Run starts background work before the --reset-password branch returns.\n" +
			"That is the ordering this package exists to prevent: the maintenance command " +
			"would start the engine and exit.")
	}
}

// findMethod returns the named method on *App from internal/app/app.go.
func findMethod(t *testing.T, name string) *ast.FuncDecl {
	t.Helper()

	path := filepath.Join(repoRoot(t), "internal", "app", "app.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv != nil && fn.Name.Name == name {
			return fn
		}
	}

	t.Fatalf("internal/app/app.go declares no method %q; if it was renamed, "+
		"this test has to be renamed with it rather than deleted", name)
	return nil
}

// calleeName reports the selector name of a call expression: for `a.svc.X()`
// and `a.X()` alike it answers "X".
func calleeName(n ast.Node) (string, bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	return sel.Sel.Name, true
}

// repoRoot walks up until it finds go.mod, so the test does not depend on where
// `go test` was invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the working directory")
		}
		dir = parent
	}
}
