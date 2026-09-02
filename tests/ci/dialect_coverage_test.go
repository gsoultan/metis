// Package ci guards the verification gate against the one failure it cannot
// detect itself: a test that never runs.
//
// This repository has shipped that failure twice. Once when the dialect suites
// skipped in CI because no DSN reached them, and once when the whole suite went
// green in 34 seconds off a warm cache having opened no connections. The
// dialects job now asserts against both — but only for the packages somebody
// remembered to list in it.
//
// That list is the remaining hole, and it is invisible by construction: a new
// dialect-gated package added to tests/ runs in the ordinary suite, skips its
// database subtests for want of a DSN, reports "ok", and is never run against
// PostgreSQL, MySQL or SQL Server by anything. It was already true of
// tests/migrations and tests/outage, and became true of tests/replicas the day
// it was written — whose conditional-insert claim is exactly the dialect-
// specific code that needs it, and which had a MySQL-only bug when it landed.
package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// dialectHelpers are the calls that make a package need a real database. Using
// any of them means the package skips, or silently falls back, without a DSN.
var dialectHelpers = []string{
	"SetupPostgresDB",
	"SetupMySQLDB",
	"SetupSQLServerDB",
	"PostgresDSNEnv",
	"MySQLDSNEnv",
	"SQLServerDSNEnv",
}

// notGatedByADSN lists packages that name a helper without depending on one.
var notGatedByADSN = map[string]string{
	// The helpers themselves; their own tests cover DSN parsing, not a database.
	"testutils": "defines the helpers rather than using them",
	// Adapts rather than skips: it measures against SQLite when no DSN is set,
	// so it is never silently unrun.
	"slo": "falls back to SQLite instead of skipping",
	// This guard.
	"ci": "reads the workflow rather than a database",
	// Gated twice on purpose: a DSN is not enough, METIS_LOADTEST must also be
	// set. It seeds hundreds of thousands of rows and asserts latency, and a
	// latency assertion on a shared runner measures the runner. Run it
	// deliberately, against hardware you are actually asking about.
	"loadtest": "opt-in via METIS_LOADTEST; latency on a shared CI runner would measure the runner",
}

const workflowPath = "../../.github/workflows/security_reliability_ci.yml"

// TestEveryDialectGatedPackageRunsInCI is the guard.
//
// It reads the workflow rather than trusting a list kept alongside it, because
// a second copy of the answer is a second thing to forget.
func TestEveryDialectGatedPackageRunsInCI(t *testing.T) {
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read the CI workflow: %v", err)
	}
	job := dialectsJob(t, string(workflow))

	for _, pkg := range dialectGatedPackages(t) {
		if reason, exempt := notGatedByADSN[pkg]; exempt {
			t.Logf("%s is exempt: %s", pkg, reason)
			continue
		}
		if !strings.Contains(job, "./tests/"+pkg+"/") {
			t.Errorf(
				"tests/%s needs a real database but the dialects job does not run it.\n"+
					"It will skip in the ordinary suite for want of a DSN, report ok, and never be run\n"+
					"against PostgreSQL, MySQL or SQL Server by anything.\n"+
					"Add ./tests/%s/... to the 'Run the dialect suites' step.", pkg, pkg)
		}
	}
}

// dialectGatedPackages returns the packages under tests/ that need a database.
func dialectGatedPackages(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir("..")
	if err != nil {
		t.Fatalf("read the tests directory: %v", err)
	}

	var gated []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if usesADialectHelper(t, filepath.Join("..", entry.Name())) {
			gated = append(gated, entry.Name())
		}
	}
	if len(gated) == 0 {
		t.Fatal("found no dialect-gated packages at all, so this guard is asserting nothing")
	}
	return gated
}

func usesADialectHelper(t *testing.T, dir string) bool {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		source, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		for _, helper := range dialectHelpers {
			if strings.Contains(string(source), helper) {
				return true
			}
		}
	}
	return false
}

// dialectsJob returns the step that runs the dialect suites.
//
// Scoped to that step rather than the whole file: a package named anywhere in
// the workflow — in a comment, or in the ordinary module-wide run — would
// otherwise satisfy this check while never meeting a real database.
func dialectsJob(t *testing.T, workflow string) string {
	t.Helper()

	const marker = "Run the dialect suites"
	start := strings.Index(workflow, marker)
	if start < 0 {
		t.Fatalf("the workflow has no %q step; this guard is reading for something that moved", marker)
	}
	rest := workflow[start:]
	// The step ends at the next step, which is the next line starting a list
	// item at the same indentation.
	if end := strings.Index(rest, "\n      - name:"); end > 0 {
		return rest[:end]
	}
	return rest
}

// TestTheSkipAndCacheAssertionsAreStillThere pins the two checks the dialects
// job makes about itself. Both failures look exactly like success — a skipped
// test and a cached one each print "ok" — so removing either would restore a
// green build that proves nothing.
func TestTheSkipAndCacheAssertionsAreStillThere(t *testing.T) {
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read the CI workflow: %v", err)
	}
	job := dialectsJob(t, string(workflow))

	for _, required := range []struct{ needle, why string }{
		{"--- SKIP", "a skipped dialect test means its DSN never arrived, which is the failure this job exists to prevent"},
		{"(cached)", "a cached run opens no connections and prints no test lines, so every other assertion here passes by having nothing to read"},
		{"-count=1", "without it Go serves the cache, because the test cache models the code and not which database answered"},
	} {
		if !strings.Contains(job, required.needle) {
			t.Errorf("the dialects job no longer checks for %q: %s", required.needle, required.why)
		}
	}
}

// TestTheWorkflowExportsTheDSNNamesTheHelpersRead cross-checks the two halves of
// the DSN handshake: the name a helper reads, and the name the workflow exports.
//
// These are declared in different files, in different languages, and nothing
// connected them. Renaming the project moved the workflow to METIS_TEST_* while
// the helpers still read GOBPM_TEST_*, so every dialect test skipped for want of
// a DSN that was sitting right there under another name. The suite reported ok;
// only the skip guard caught it, and only after a push.
//
// The value is not checked — only the name. Where the database lives is the
// workflow's business; agreeing on what to call it is shared.
func TestTheWorkflowExportsTheDSNNamesTheHelpersRead(t *testing.T) {
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read the CI workflow: %v", err)
	}
	job := dialectsJobBlock(t, string(workflow))

	for _, helper := range dsnHelpers() {
		name := dsnEnvName(t, helper)
		if !strings.Contains(job, name+":") {
			t.Errorf(""+
				"%s reads %s, which the Dialects job never exports.\n"+
				"Every test gated on it will skip, and the suite will still report ok.\n"+
				"Export %s in the Dialects job, or point the helper at the name that is.",
				helper, name, name)
		}
	}
}

// dsnHelpers lists the helper sources that gate a dialect behind a DSN.
func dsnHelpers() []string {
	return []string{
		"../testutils/postgres.go",
		"../testutils/mysql.go",
		"../testutils/sqlserver.go",
	}
}

// dsnEnvName reads the environment variable name a helper is gated on, out of
// the source rather than by importing it — the point is to catch a rename that
// compiles perfectly well on both sides.
func dsnEnvName(t *testing.T, path string) string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	m := dsnConstPattern.FindSubmatch(src)
	if m == nil {
		t.Fatalf("%s declares no `const ...DSNEnv = \"...\"`; this guard is reading for something that moved", path)
	}
	return string(m[1])
}

var dsnConstPattern = regexp.MustCompile(`const \w*DSNEnv = "([^"]+)"`)

// dialectsJobBlock returns the whole `dialects:` job, not just the step that
// runs the suites. The DSNs are exported at job level — sibling to `steps:` —
// so the narrower scope dialectsJob uses for package names cannot see them.
func dialectsJobBlock(t *testing.T, workflow string) string {
	t.Helper()

	const marker = "\n  dialects:\n"
	start := strings.Index(workflow, marker)
	if start < 0 {
		t.Fatalf("the workflow has no `dialects:` job; this guard is reading for something that moved")
	}
	rest := workflow[start+1:]
	// The job ends where the next one begins: a key at two-space indentation.
	if end := nextJobKey(rest); end > 0 {
		return rest[:end]
	}
	return rest
}

// nextJobKey finds the start of the following job, skipping the one we are in.
func nextJobKey(block string) int {
	for i := 1; i < len(block); i++ {
		if block[i-1] != '\n' {
			continue
		}
		line := block[i:]
		if end := strings.IndexByte(line, '\n'); end >= 0 {
			line = line[:end]
		}
		if len(line) > 2 && line[0] == ' ' && line[1] == ' ' && line[2] != ' ' && strings.HasSuffix(strings.TrimRight(line, " "), ":") {
			return i
		}
	}
	return -1
}
