package gorms

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/gsoultan/gobpm/internal/pkg/features"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
)

// The strict scope answers an unidentified query with nothing rather than an
// error. That is the right contract — a repository returns rows, and callers
// handle finding none — but it makes the flag hard to turn on: a background
// path that forgets to mark itself does not fail, it goes quiet, and an
// operator watching staging has to notice an absence.
//
// These pin the diagnostic that turns that into a list to read.

// captureLogs redirects the global logger for one test and returns what was written.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	original := zlog.Logger
	zlog.Logger = zerolog.New(&buf)
	t.Cleanup(func() { zlog.Logger = original })
	return &buf
}

// forgetReportedSites clears the once-per-site memory so tests do not depend on
// each other's order.
func forgetReportedSites(t *testing.T) {
	t.Helper()
	reportedSites = sync.Map{}
	t.Cleanup(func() { reportedSites = sync.Map{} })
}

func TestADeniedQueryNamesTheCallerThatNeedsAnIdentity(t *testing.T) {
	forgetReportedSites(t)
	defer features.OverrideForTest(features.StrictTenantScope, true)()
	logs := captureLogs(t)

	if unscopedAccessAllowed(t.Context()) {
		t.Fatal("a context with no identity was allowed while the strict scope was on")
	}

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("expected one log line, got %q", logs.String())
	}
	repository, _ := entry["repository"].(string)
	if !strings.Contains(repository, "gorms.") {
		t.Errorf("repository = %q; the warning must name the repository method, which is what says *what* came back empty", repository)
	}
	if entry["at"] == nil {
		t.Error("the warning does not give a file and line")
	}
	if !strings.Contains(entry["message"].(string), "WithSystemContext") {
		t.Error("the warning does not say how to fix it")
	}
	if entry["flag"] != "GOBPM_FEATURE_STRICT_TENANT_SCOPE" {
		t.Errorf("flag = %v, want the variable an operator would look for", entry["flag"])
	}
}

// These sit on poll loops that run every couple of seconds. The useful output
// is the list of paths needing an identity, not a count of how often they ran —
// and a warning per query would drown the log it is meant to inform.
func TestASiteIsNamedOnceHoweverOftenItRuns(t *testing.T) {
	forgetReportedSites(t)
	defer features.OverrideForTest(features.StrictTenantScope, true)()
	logs := captureLogs(t)

	for range 50 {
		_ = unscopedAccessAllowed(t.Context())
	}

	if lines := strings.Count(strings.TrimSpace(logs.String()), "\n") + 1; lines != 1 {
		t.Fatalf("50 denials produced %d log lines, want 1", lines)
	}
}

// Nothing is reported while the flag is off, which is the shipped default: the
// path is not reached, so an installation that has not opted in pays nothing
// and sees nothing.
func TestNothingIsReportedWhileTheFlagIsOff(t *testing.T) {
	forgetReportedSites(t)
	defer features.OverrideForTest(features.StrictTenantScope, false)()
	logs := captureLogs(t)

	if !unscopedAccessAllowed(t.Context()) {
		t.Fatal("the default refused an unidentified query")
	}
	if logs.Len() != 0 {
		t.Fatalf("the default logged %q; an installation that has not opted in should see nothing", logs.String())
	}
}

// System work is the legitimate case and must not be reported, or the list an
// operator reads would be mostly noise from paths that are already correct.
func TestSystemWorkIsNotReported(t *testing.T) {
	forgetReportedSites(t)
	defer features.OverrideForTest(features.StrictTenantScope, true)()
	logs := captureLogs(t)

	if !unscopedAccessAllowed(entities.WithSystemContext(t.Context())) {
		t.Fatal("marked system work was refused")
	}
	if logs.Len() != 0 {
		t.Fatalf("system work was reported as needing an identity: %q", logs.String())
	}
}
