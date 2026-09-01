// Package envvar reads this service's configuration from the environment,
// honouring the names it used to have.
//
// The product is Metis and its variables are METIS_-prefixed. They were
// GOBPM_-prefixed, and a rename that simply dropped the old names would be a
// silent break: an installation with GOBPM_FEATURE_JAVASCRIPT_CONDITIONS set
// would come back up with the flag at its default, and every gateway routing
// on a `js:` condition would stop — with nothing in the log to connect that to
// an upgrade. The same shape applies to every listen address, every timeout and
// every egress allowlist.
//
// So the old name is still read, and using it says so once. The fallback is a
// migration aid with an expiry, not a second supported spelling: see
// docs/upgrading.md for when it goes.
package envvar

import (
	"os"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

const (
	// prefix is what this service's variables are called now.
	prefix = "METIS_"

	// legacyPrefix is what they were called before the rename.
	legacyPrefix = "GOBPM_"
)

// warned remembers which legacy names have already been reported.
//
// Keyed by variable name, so it is bounded by the number of settings rather
// than by traffic — some of these are read on every outbound request, and a
// line per read would bury the message it exists to deliver.
var warned sync.Map

// Lookup returns the value of name, falling back to its pre-rename spelling.
//
// name is always the current METIS_ spelling; callers do not know the old one.
// The boolean distinguishes "set to empty" from "not set", which several
// callers depend on to tell a deliberate blank from an absent setting.
func Lookup(name string) (string, bool) {
	if value, ok := os.LookupEnv(name); ok {
		return value, true
	}

	legacy, ok := legacyNameFor(name)
	if !ok {
		return "", false
	}
	value, ok := os.LookupEnv(legacy)
	if !ok {
		return "", false
	}

	reportLegacyUse(legacy, name)
	return value, true
}

// Get returns the value of name, or "" when neither spelling is set.
func Get(name string) string {
	value, _ := Lookup(name)
	return value
}

// legacyNameFor maps a current name to the one it replaced. A name outside this
// service's namespace — ENCRYPTION_KEY, JWT_SECRET, DATABASE_URL, which were
// never prefixed — has no predecessor and is read as-is.
func legacyNameFor(name string) (string, bool) {
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	return legacyPrefix + strings.TrimPrefix(name, prefix), true
}

// reportLegacyUse names the variable to change, once per variable.
func reportLegacyUse(legacy, current string) {
	if _, seen := warned.LoadOrStore(legacy, struct{}{}); seen {
		return
	}
	log.Warn().
		Str("using", legacy).
		Str("rename_to", current).
		Msg("This setting is read under its old name. GoBPM is now Metis; the GOBPM_ spelling still works and will be removed in a future release.")
}
