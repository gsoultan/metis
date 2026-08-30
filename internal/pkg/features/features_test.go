package features

import (
	"os"
	"sync"
	"testing"
)

func TestEnabled(t *testing.T) {
	tests := []struct {
		name  string
		value string
		set   bool
		want  bool
	}{
		{name: "unset uses the default", want: StrictTenantScope.Default},
		{name: "true", value: "true", set: true, want: true},
		{name: "1", value: "1", set: true, want: true},
		{name: "false", value: "false", set: true, want: false},
		{name: "surrounding space is tolerated", value: "  true  ", set: true, want: true},
		// An unreadable value must not be read as "on". A typo in a deployment
		// manifest should leave the safe default in place, not enable a change
		// nobody asked for.
		{name: "nonsense falls back to the default", value: "yes-please", set: true, want: StrictTenantScope.Default},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetForTest()
			t.Cleanup(resetForTest)
			if tc.set {
				t.Setenv(EnvName(StrictTenantScope), tc.value)
			} else {
				// Explicitly unset rather than merely not setting: the test
				// process may already carry the variable — it does when the
				// suite is run with strict scoping enabled — and "unset" is the
				// case being tested.
				unsetForTest(t, EnvName(StrictTenantScope))
			}

			if got := Enabled(StrictTenantScope); got != tc.want {
				t.Errorf("Enabled(%s) = %v, want %v", StrictTenantScope.Name, got, tc.want)
			}
		})
	}
}

func TestEnvName(t *testing.T) {
	if got := EnvName(StrictTenantScope); got != "METIS_FEATURE_STRICT_TENANT_SCOPE" {
		t.Errorf("EnvName = %q, want METIS_FEATURE_STRICT_TENANT_SCOPE", got)
	}
}

// A flag set under the pre-rename name must keep working. Getting this wrong is
// how an upgrade silently turns a security control off: an installation with
// GOBPM_FEATURE_JAVASCRIPT_CONDITIONS=true would come back with the flag at its
// default, and every gateway routing on a `js:` condition would stop.
func TestAFlagSetUnderItsOldNameIsStillRead(t *testing.T) {
	resetForTest()
	t.Cleanup(resetForTest)

	unsetForTest(t, EnvName(JavaScriptConditions))
	t.Setenv("GOBPM_FEATURE_JAVASCRIPT_CONDITIONS", "true")

	if !Enabled(JavaScriptConditions) {
		t.Fatal("a flag set under GOBPM_FEATURE_ was ignored; upgrading would silently change behaviour")
	}
}

// TestSecurityDefaults locks the shipped posture. Changing either default is a
// security decision with a rollout plan (the flag comments say what has to be
// true first), so it must cost a deliberate edit here, not slip through as a
// tweak.
func TestSecurityDefaults(t *testing.T) {
	if JavaScriptConditions.Default {
		t.Error("javascript-conditions must default off: goja cannot be memory-bounded, and a default install must not be one definition away from that")
	}
	if StrictTenantScope.Default {
		t.Error("strict-tenant-scope must stay opt-in until every background entry point provably carries an identity; flipping it is a staged rollout, not a default change")
	}
}

// TestResolutionIsStable pins the once-only read. A flag that could change under
// a running process would let one request take the old path and the next the
// new one — and for a security control, that makes "was this query scoped"
// depend on timing.
func TestResolutionIsStable(t *testing.T) {
	resetForTest()
	t.Cleanup(resetForTest)

	t.Setenv(EnvName(StrictTenantScope), "true")
	first := Enabled(StrictTenantScope)

	t.Setenv(EnvName(StrictTenantScope), "false")
	if second := Enabled(StrictTenantScope); second != first {
		t.Fatalf("the flag changed under a running process: %v then %v", first, second)
	}
}

// TestEnabledIsConcurrencySafe guards the lazy resolution, which is read from
// every repository query on every goroutine the engine runs.
func TestEnabledIsConcurrencySafe(t *testing.T) {
	resetForTest()
	t.Cleanup(resetForTest)

	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() { _ = Enabled(StrictTenantScope) })
	}
	wg.Wait()
}

// TestEveryFlagDocumentsItsRetirement keeps the package from becoming a
// graveyard: a flag with no stated exit is one nobody will ever dare delete.
func TestEveryFlagDocumentsItsRetirement(t *testing.T) {
	for _, f := range all {
		if f.Why == "" {
			t.Errorf("flag %q does not say what it is for", f.Name)
		}
		if f.Retire == "" {
			t.Errorf("flag %q does not say when it can be removed", f.Name)
		}
	}
}

// unsetForTest removes an environment variable for the duration of a test and
// restores it afterwards. testing has Setenv but no Unsetenv, and a test about
// "what happens when this is not configured" cannot rely on the ambient
// environment not configuring it.
func unsetForTest(t *testing.T, key string) {
	t.Helper()
	previous, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, previous)
			return
		}
		_ = os.Unsetenv(key)
	})
}
