// Package features carries the flags that let a risky change ship dark.
//
// .junie/guidelines.md §6 asks for small, reversible changes with feature flags
// for risky behaviour. The changes that need one here have a particular shape:
// they are correct, and they alter what existing deployments do. Strict tenant
// scoping is the example — denying a query that has no tenant identity is the
// right rule, and switching it on without a way back turns a mistake in one
// background entry point into an engine that silently stops working.
//
// Deliberately small. Flags are booleans read from the environment once at
// startup, because the alternative — a service, a cache, a refresh loop — is a
// new runtime dependency in the request path, and the thing it would be gating
// is a security control.
//
// A flag is a debt with a due date. Each one below records what has to be true
// before it can be deleted, since the failure mode of a flag package is that it
// only ever grows.
package features

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

// Flag is one named, reversible behaviour switch.
type Flag struct {
	// Name is the flag's identity. The environment variable is
	// GOBPM_FEATURE_<NAME>, upper-cased with dashes as underscores.
	Name string

	// Default is what an operator who has configured nothing gets. A flag
	// guarding a change that alters existing behaviour defaults to off; the
	// point is that upgrading changes nothing until someone decides otherwise.
	Default bool

	// Why records what the flag is for, and Retire records what must be true
	// before it can be removed. Without the second, flags accumulate until
	// nobody can say which are still load-bearing.
	Why    string
	Retire string
}

// StrictTenantScope makes a repository query with no tenant identity return
// nothing instead of everything.
//
// The repository scope currently returns unscoped results when a context carries
// no TenantContext, which is what lets the engine and its background workers
// read across tenants in order to do their work. That contradicts AGENTS §2.3
// (absent constraint means deny), and it means any code path that forgets to
// carry identity gets full access rather than an error.
//
// Turning it on requires that every legitimate background entry point marks
// itself as system work. Missing one does not fail loudly — it makes queries
// return nothing — so this ships off, gets switched on in a staging environment
// first, and only then in production.
//
// **The staging step is now readable rather than a vigil.** Every denial logs a
// warning naming the repository method and the path that called it without an
// identity, once per site. So the rollout is: turn it on in staging, exercise
// the product, and read the warnings — each one is a path that needs
// entities.WithSystemContext or a resolved tenant. When none appear, the flag
// is safe to carry to production. Before that diagnostic existed an operator
// had to notice an absence, which is the thing people miss.
var StrictTenantScope = Flag{
	Name:    "strict-tenant-scope",
	Default: false,
	Why:     "deny repository queries that carry neither a tenant nor a system identity",
	Retire:  "delete once production has run with it on through a full release cycle; then make the behaviour unconditional",
}

// JavaScriptConditions allows `js:` gateway conditions to run.
//
// Conditions are authored in deployed definitions, and a JavaScript one is
// handed to a runtime that cannot be fully bounded: goja honours interrupts
// only between statements, so a single native call runs to completion —
// measured at 37 seconds against a 200ms budget, allocating freely the whole
// time. FEEL, which now handles conditions natively, has no construct that can
// do that.
//
// It defaults OFF: a default install must not be one deployed definition away
// from memory exhaustion no sandbox setting can bound. The cost of off is that
// a definition still using `js:` has a decision point that refuses to route —
// loudly, with an error log naming this flag — rather than a silent wrong
// answer. GET /api/v1/definitions/javascript-conditions lists every stored
// condition the flag gates, so an installation that must turn this on can see
// exactly what it is buying time for, and when the worklist is empty enough to
// turn it back off.
var JavaScriptConditions = Flag{
	Name:    "javascript-conditions",
	Default: false,
	Why:     "allow js: gateway conditions, which FEEL has replaced",
	Retire:  "delete once no supported installation still turns it on; then drop the JS evaluator from the condition chain",
}

// all is the registry, used to report configuration at startup.
var all = []*Flag{&StrictTenantScope, &JavaScriptConditions}

var (
	resolveOnce sync.Once
	resolved    map[string]bool
)

// Enabled reports whether a flag is on.
//
// Resolution happens once. A flag that could change under a running process
// would let one request take the old path and the next the new one, which for a
// security control means the answer to "was this scoped" depends on timing.
func Enabled(f Flag) bool {
	resolveOnce.Do(resolveAll)
	return resolved[f.Name]
}

// resolveAll reads every flag from the environment and logs the ones that are
// not at their default, so a surprising deployment can be diagnosed from its
// startup log rather than by guessing.
func resolveAll() {
	resolved = make(map[string]bool, len(all))
	for _, f := range all {
		value := f.Default
		if raw, ok := os.LookupEnv(envName(f.Name)); ok {
			parsed, err := strconv.ParseBool(strings.TrimSpace(raw))
			if err != nil {
				log.Warn().
					Str("flag", f.Name).
					Str("value", raw).
					Msg("Ignoring an unreadable feature flag value; using the default")
			} else {
				value = parsed
			}
		}
		resolved[f.Name] = value

		if value != f.Default {
			log.Info().
				Str("flag", f.Name).
				Bool("enabled", value).
				Str("why", f.Why).
				Msg("Feature flag is not at its default")
		}
	}
}

// envName maps a flag name to its environment variable.
func envName(name string) string {
	return "GOBPM_FEATURE_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

// EnvName exposes the variable that controls a flag, for documentation and
// error messages that would otherwise have to spell it out and drift.
func EnvName(f Flag) string { return envName(f.Name) }

// resetForTest clears the resolved cache. Tests need this because resolution is
// deliberately once-only for the running process.
func resetForTest() {
	resolveOnce = sync.Once{}
	resolved = nil
}

// OverrideForTest forces a flag's value and returns a function restoring it.
//
// Setting the environment variable is not enough from another package:
// resolution happens once per process, and by the time a test runs some earlier
// test has usually already triggered it. That is the correct production
// behaviour — a flag that changed under a running process would make "was this
// query scoped" depend on timing — so tests get an explicit door rather than the
// behaviour being loosened for them.
//
//	defer features.OverrideForTest(features.StrictTenantScope, true)()
//
// It returns a restore function rather than taking a *testing.T so that this
// package does not import testing: a production binary should not carry the test
// framework just to make a flag overridable.
func OverrideForTest(f Flag, value bool) func() {
	resolveOnce.Do(resolveAll)
	previous, existed := resolved[f.Name]
	resolved[f.Name] = value

	return func() {
		if existed {
			resolved[f.Name] = previous
			return
		}
		delete(resolved, f.Name)
	}
}
