package app

import (
	"context"
	"os"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/internal/pkg/releasecheck"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories/migrations"
)

const (
	envUpgradeCheck   = "METIS_UPGRADE_CHECK"
	envUpgradeChannel = "METIS_UPGRADE_CHANNEL"
	// upgradeCheckInterval is how often the server asks GitHub. A release is a
	// weekly event at most; four requests a day is well inside the sixty an
	// hour GitHub allows a host without a token.
	upgradeCheckInterval = 6 * time.Hour

	// upgradeTriggerEnabled exists while metis-upgrade.path is enabled.
	upgradeTriggerEnabled = "/etc/systemd/system/paths.target.wants/metis-upgrade.path"
)

// upgradeService builds the Upgrades page's service, and the checker that
// startUpgradeChecks runs, from METIS_UPGRADE_CHECK and METIS_UPGRADE_CHANNEL.
func (a *App) upgradeService() servicecontracts.UpgradeService {
	channel := "stable"
	switch value := envvar.Get(envUpgradeChannel); value {
	case "", "stable":
	case "prerelease":
		channel = value
	default:
		log.Warn().Str("value", value).Msg(envUpgradeChannel + " is neither stable nor prerelease; offering final releases only")
	}

	settings := serviceimpl.UpgradeSettings{
		Current:       version,
		Channel:       channel,
		Install:       serviceimpl.DetectInstall(),
		SchemaVersion: appliedSchemaVersion(),
	}
	if settings.Install == servicecontracts.InstallPackage {
		settings.RequestPath = UpgradeRequestFile
		settings.StatusPath = UpgradeStatusFile
		// Enabled, not merely installed: an operator who disabled it chose the
		// command line.
		_, err := os.Stat(upgradeTriggerEnabled)
		settings.Triggered = err == nil
	}
	switch value := envvar.Get(envUpgradeCheck); value {
	case "", "true":
		// Not httpclient's guarded client: that one fences off the addresses a
		// process definition must never reach, which includes a proxy on this
		// host, and would refuse every check made through one. The address
		// here is a constant in this source, not anything a user wrote.
		a.releaseChecker = releasecheck.New(releasecheck.Options{
			Current:    version,
			Prerelease: channel == "prerelease",
		})
		settings.Checker = a.releaseChecker
	case "false":
	default:
		// Off rather than on: an operator who wrote something here meant to
		// change the default, and the default is to call out.
		log.Warn().Str("value", value).Msg(envUpgradeCheck + " is neither true nor false; not checking for new releases")
	}
	return serviceimpl.NewUpgradeService(settings)
}

// appliedSchemaVersion is the migration this binary brings a database to,
// which is where the database is once it has started.
func appliedSchemaVersion() int64 {
	latest := int64(migrations.ConvertedVersion)
	for _, step := range migrations.Steps() {
		latest = max(latest, step.Version)
	}
	return latest
}

// startUpgradeChecks asks GitHub for new releases now and every six hours.
func (a *App) startUpgradeChecks(ctx context.Context) {
	if a.releaseChecker == nil {
		return
	}
	go a.releaseChecker.Run(ctx, upgradeCheckInterval, func(err error) {
		log.Warn().Err(err).Msg("Could not check for a new release of Metis; will try again in six hours")
	})
}
