// Package distribution names a program built from this engine as it is
// published and installed: its GitHub repository, where its releases are
// signed and downloaded, and its Linux package, which fixes the binary, the
// service, the account and the directories a package install uses.
//
// The metis command is one distribution; metis-server, which embeds the
// engine, is another. Everything that reads releases or upgrades a package
// install takes its names from here, so an embedding program is compared with
// its own releases and upgraded with its own packages, never the engine's.
package distribution

import (
	"fmt"
	"regexp"
)

// Distribution is one published program.
type Distribution struct {
	// Name is the Linux package's name, and with it the binary in /usr/bin,
	// the service, the account and group, /etc/<Name> and /var/lib/<Name>.
	Name string
	// Repository is the GitHub repository, owner/name, whose release workflow
	// builds, signs and publishes the program's releases.
	Repository string
}

// Metis is the metis command, published from this repository.
var Metis = Distribution{Name: "metis", Repository: "gsoultan/metis"}

// GitHub's limits, and nothing a URL or a command line would read as anything
// but a name.
var (
	namePattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$`)
)

// Validate refuses a name or repository that is not one. Both go into URLs,
// file paths, a signing identity and package manager commands.
func (d Distribution) Validate() error {
	if !namePattern.MatchString(d.Name) {
		return fmt.Errorf("distribution name %q is not a package name (lowercase letters, digits and dashes)", d.Name)
	}
	if !repositoryPattern.MatchString(d.Repository) || d.Repository[len(d.Repository)-1] == '.' {
		return fmt.Errorf("distribution repository %q is not a GitHub owner/name", d.Repository)
	}
	return nil
}
