package ci

import (
	"os"
	"strings"
	"testing"
)

// provenUnderTheStrictScope are the packages that exercise product paths
// through the real interceptor chain and pass with the flag forced on.
//
// Each was verified to pass under METIS_FEATURE_STRICT_TENANT_SCOPE=true before
// being listed. They are the entire safety net for the flag's rollout: the rest
// of the suite calls services directly with a bare context, which production
// never does, so it fails under the flag for reasons that say nothing about
// production and cannot detect a regression either.
//
// Dropping one from the Makefile would shrink that net silently — the target
// would still be green, and would still be named strict-scope.
var provenUnderTheStrictScope = []string{
	"./tests/strictscope/...",
	"./tests/slo/...",
	"./tests/user/...",
	"./tests/setup/...",
	"./tests/outage/...",
	"./tests/replicas/...",
	"./tests/auth/...",
}

const makefilePath = "../../Makefile"

// TestTheStrictScopeTargetStillRunsEveryProvenPackage reads the Makefile rather
// than trusting a list kept beside it.
func TestTheStrictScopeTargetStillRunsEveryProvenPackage(t *testing.T) {
	makefile, err := os.ReadFile(makefilePath)
	if err != nil {
		t.Fatalf("read the Makefile: %v", err)
	}
	body := strictScopeVariable(t, string(makefile))

	for _, pkg := range provenUnderTheStrictScope {
		if !strings.Contains(body, pkg) {
			t.Errorf(""+
				"STRICT_SCOPE_PKGS no longer runs %s.\n"+
				"It passed under METIS_FEATURE_STRICT_TENANT_SCOPE=true when it was added, so removing it\n"+
				"shrinks the only net that can catch a path losing its tenant identity — and `make strict-scope`\n"+
				"stays green while covering less. Put it back, or delete it from provenUnderTheStrictScope\n"+
				"and say in the commit why that coverage is no longer wanted.",
				pkg)
		}
	}
}

// TestTheGateStillRunsTheStrictScopeTarget pins the target into the gate.
//
// A suite that exists but is not run is the failure this repository has already
// had twice: once with the dialect suites, once with the whole tests/ tree.
func TestTheGateStillRunsTheStrictScopeTarget(t *testing.T) {
	makefile, err := os.ReadFile(makefilePath)
	if err != nil {
		t.Fatalf("read the Makefile: %v", err)
	}
	for _, line := range strings.Split(string(makefile), "\n") {
		if strings.HasPrefix(line, "gate:") {
			if !strings.Contains(line, "strict-scope") {
				t.Fatal("the gate no longer runs strict-scope, so nothing proves the flag is still safe to turn on")
			}
			return
		}
	}
	t.Fatal("the Makefile has no `gate:` target; this guard is reading for something that moved")
}

// strictScopeVariable returns the STRICT_SCOPE_PKGS assignment, following
// backslash continuations.
func strictScopeVariable(t *testing.T, makefile string) string {
	t.Helper()

	const marker = "STRICT_SCOPE_PKGS"
	start := strings.Index(makefile, marker)
	if start < 0 {
		t.Fatalf("the Makefile has no %s; this guard is reading for something that moved", marker)
	}

	var body strings.Builder
	for _, line := range strings.Split(makefile[start:], "\n") {
		body.WriteString(line)
		if !strings.HasSuffix(strings.TrimRight(line, " \t"), "\\") {
			break
		}
	}
	return body.String()
}
