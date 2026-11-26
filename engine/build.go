package engine

import "github.com/gsoultan/metis/internal/pkg/distribution"

// Build is the program that embeds the engine, as it is published: the
// version it was built as, and where its releases and Linux packages come
// from.
type Build struct {
	// Version is the program's own version, such as v0.2.2: the tag it was
	// built from, stamped at link time. Empty keeps the engine's own stamp,
	// "dev" when there is none.
	Version string
	// Name is the program's Linux package, and with it its binary in
	// /usr/bin, its service and account, /etc/<Name> and /var/lib/<Name>.
	Name string
	// Repository is the GitHub repository, owner/name, whose release workflow
	// signs and publishes the program's releases.
	Repository string
}

// WithBuild has the program that embeds the engine report its own version,
// and compare itself with and upgrade itself from its own releases, rather
// than the metis command's. The liveness probe, traces and the Upgrades page
// all report Version, the new-release check reads Repository's releases, and
// `<Name> upgrade` installs Name's packages, accepting only a checksums.txt
// signed by Repository's release workflow for the tag.
//
// Run fails when Name or Repository is not one.
func WithBuild(build Build) Option {
	return func(e *Engine) {
		e.app.UseBuild(build.Version, distribution.Distribution{Name: build.Name, Repository: build.Repository})
	}
}
