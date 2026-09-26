// Package tenantscope decides what a context carrying no tenant may see, and
// names the code paths that reach a repository without one.
//
// It lives outside the persistence layer because two of them read it: the GORM
// repositories, and the storm repositories replacing them. A copy in each would
// be two answers to "may this query run", and the one that drifts is whichever
// is edited less.
//
// It also keeps a request's resolved scope for the length of the request
// (Request): started by the tenant resolver, which is what knows when a request
// begins and ends, and read by the repositories, which are what resolve it.
package tenantscope

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/gsoultan/metis/internal/pkg/features"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/rs/zerolog/log"
)

// Allowed decides what a context carrying no tenant may see.
//
// System work — the engine, the job worker, message consumers, migrations —
// legitimately spans every tenant and says so explicitly. Anything else is a
// path that failed to resolve a tenant, which AGENTS §2.3 says must deny.
//
// Behind a flag because the failure mode of getting it wrong is quiet: a
// background entry point that forgets to mark itself does not error, it reads
// nothing, and an engine that reads nothing looks like an engine with no work.
// Off by default, so upgrading changes nothing until somebody decides otherwise.
func Allowed(ctx context.Context) bool {
	if entities.IsSystemContext(ctx) {
		return true
	}
	if !features.Enabled(features.StrictTenantScope) {
		return true
	}
	Report()
	return false
}

// reportedSites remembers which call sites have already been named.
//
// Keyed by program counter, so it is bounded by the amount of code that can
// reach here rather than by traffic — this is not a map keyed on anything a
// caller supplies.
var reportedSites sync.Map

// packagePrefixes are the persistence packages a denial is reported *through*.
// A frame in one of them is scaffolding; the interesting caller is the first
// frame outside them.
//
// This package's own entry point is named exactly rather than by prefix. A
// prefix would also match every test in this package, so the walk would skip
// past the caller it is supposed to name and report the test runner.
var packagePrefixes = []string{
	"/server/repositories/gorms.",
	"/server/repositories/pg.",
}

const ownEntryPoint = "tenantscope.Allowed"

// Report names the code path that reached a repository with neither a tenant
// nor a system identity.
//
// **The strict scope's failure mode is silence.** A background entry point that
// forgets to mark itself does not error — it reads nothing, and an engine that
// reads nothing looks like an engine with no work to do. That is precisely what
// makes turning the flag on hard to evaluate: an operator watching a staging
// environment has to notice an *absence*, and absences are what people miss.
//
// So each distinct site says so. Once, not every time: these sit on poll loops
// that run every couple of seconds, and the useful output is the list of paths
// that still need an identity — not a count of how often they ran. Turning a
// rollout from "watch for something that stops happening" into "read this list"
// is the whole point.
func Report() {
	site, from, ok := deniedCallSite()
	if !ok {
		return
	}
	denial := fmt.Sprintf("%s (%s:%d)", site.Function, site.File, site.Line)
	if from != "" {
		denial += " called from " + from
	}
	if _, seen := reportedSites.LoadOrStore(site.PC, denial); seen {
		return
	}
	event := log.Warn().
		Str("repository", site.Function).
		Str("at", fmt.Sprintf("%s:%d", site.File, site.Line)).
		Str("flag", features.EnvName(features.StrictTenantScope))
	if from != "" {
		// The repository method says *what* was denied; its caller says which
		// path forgot an identity, which is the thing that has to change.
		event = event.Str("called_from", from)
	}
	event.Msg("A repository query carried neither a tenant nor a system identity, so it was answered with nothing. " +
		"This path needs entities.WithSystemContext if it is background work, or a resolved tenant if it serves a request.")
}

// ForgetForTest clears the once-per-site memory so tests do not depend on each
// other's order.
func ForgetForTest() { reportedSites = sync.Map{} }

// deniedCallSite returns the repository method that was denied and, when it can
// be told, the caller outside the persistence layer that invoked it.
//
// Both, because they answer different questions. The repository method says
// what came back empty; its caller is the path that failed to carry an identity
// and therefore the code that has to change.
func deniedCallSite() (site runtime.Frame, calledFrom string, ok bool) {
	pc := make([]uintptr, 24)
	// Skip runtime.Callers, this function and Report.
	n := runtime.Callers(3, pc)
	if n == 0 {
		return runtime.Frame{}, "", false
	}

	frames := runtime.CallersFrames(pc[:n])
	for {
		frame, more := frames.Next()
		switch {
		case site.PC == 0 && inPersistence(frame.Function):
			// Scaffolding on the way in; keep looking for the repository method.
		case site.PC == 0:
			site = frame
		case !inPersistence(frame.Function):
			return site, frame.Function, true
		}
		if !more {
			return site, calledFrom, site.PC != 0
		}
	}
}

func inPersistence(function string) bool {
	if strings.HasSuffix(function, ownEntryPoint) {
		return true
	}
	for _, prefix := range packagePrefixes {
		if strings.Contains(function, prefix) {
			return true
		}
	}
	return false
}

// DeniedSites is every path reported so far.
//
// Empty is the goal. Anything in here is a path that needs
// entities.WithSystemContext if it is background work, or a resolved tenant if
// it serves a request.
func DeniedSites() []string {
	var sites []string
	reportedSites.Range(func(_, value any) bool {
		if denial, ok := value.(string); ok {
			sites = append(sites, denial)
		}
		return true
	})
	return sites
}

// ResetDeniedSites forgets what has been reported.
//
// Only useful to a test that wants to attribute denials to one specific
// exercise: the deduplication is per process, so without a reset the second
// test to run sees whatever the first provoked and cannot tell them apart.
func ResetDeniedSites() {
	reportedSites.Range(func(key, _ any) bool {
		reportedSites.Delete(key)
		return true
	})
}
