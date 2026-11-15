// Package webapp finds the web app's source for the tests that check the
// engine against it.
//
// Some rules are written down on both sides: the roles the interface offers,
// the secret fragments the browser hides, the words for each access area.
// The tests in tests/roledrift and tests/secretdrift read the web app's own
// source rather than a copy, so the two cannot drift apart. The web app lives
// in gsoultan/metis-server, so they need a checkout of it: METIS_SERVER_DIR,
// or ../metis-server next to this repository. CI checks one out.
package webapp

import (
	"os"
	"path/filepath"
	"runtime"
)

// File is the path of rel (such as "ui/src/domain/roles.ts") inside the
// metis-server checkout.
//
// A missing checkout is not skipped: the test reading the file fails, naming
// this path, because a drift check that quietly stops running is how the two
// sides drifted before.
func File(rel string) string {
	return filepath.Join(Root(), filepath.FromSlash(rel))
}

// Root is the metis-server checkout: METIS_SERVER_DIR, or ../metis-server
// beside this repository.
func Root() string {
	if dir := os.Getenv("METIS_SERVER_DIR"); dir != "" {
		return filepath.Clean(dir)
	}
	_, here, _, _ := runtime.Caller(0)
	// here is <repo>/tests/webapp/webapp.go.
	return filepath.Join(filepath.Dir(here), "..", "..", "..", "metis-server")
}
