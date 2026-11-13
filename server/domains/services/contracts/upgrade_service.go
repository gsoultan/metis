package contracts

import (
	"context"

	"github.com/gsoultan/metis/internal/pkg/releasecheck"
	"github.com/gsoultan/metis/internal/pkg/releasemanifest"
)

// How Metis was installed on this host, which decides how it is upgraded.
const (
	// InstallPackage is a deb, rpm, apk or Arch package run by the init
	// system: the upgrade helper can install a new release.
	InstallPackage = "package"
	// InstallContainer is an image run by Docker, Compose or Kubernetes,
	// which owns the image: Metis only says what to move to.
	InstallContainer = "container"
	// InstallBinary is a binary placed by hand: whoever placed it replaces it.
	InstallBinary = "binary"
)

// UpgradeStatus is what the Upgrades page shows.
type UpgradeStatus struct {
	releasecheck.Status
	// Enabled is false when METIS_UPGRADE_CHECK turns the check off; nothing
	// is asked of GitHub then, and Newest stays empty.
	Enabled bool `json:"enabled"`
	// Channel is "stable", or "prerelease" to be offered release candidates.
	Channel string `json:"channel"`
	// Install is how this host runs Metis: InstallPackage, InstallContainer
	// or InstallBinary.
	Install string `json:"install"`
	// SchemaVersion is the migration the database was brought to when this
	// binary started.
	SchemaVersion int64 `json:"schema_version"`
	// Pending is what the newest release would apply to this database, when
	// its manifest could be read.
	Pending []releasemanifest.Migration `json:"pending"`
	// Reachable is false when the database is older than the newest release
	// upgrades from, so a release in between has to be started first.
	Reachable bool `json:"reachable"`
}

// UpgradeService finds newer releases of Metis for the Upgrades page.
//
// Both methods are a platform administrator's: which version an installation
// runs, and what an upgrade would change, is information about every
// organization on it.
type UpgradeService interface {
	// UpgradeStatus is the answer of the last check, without asking again.
	UpgradeStatus(ctx context.Context) (UpgradeStatus, error)
	// CheckForUpgrade asks GitHub now, at most once a minute.
	CheckForUpgrade(ctx context.Context) (UpgradeStatus, error)
}
