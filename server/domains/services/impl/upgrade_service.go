package impl

import (
	"context"
	"os"
	"time"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/releasecheck"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// minCheckInterval is how soon "Check now" may ask GitHub again. Unsigned
// requests are limited to sixty an hour for the whole host.
const minCheckInterval = time.Minute

// UpgradeSettings is what the upgrade service is built with.
type UpgradeSettings struct {
	// Checker reads GitHub. Nil when the check is turned off.
	Checker       *releasecheck.Checker
	Current       string
	Channel       string
	Install       string
	SchemaVersion int64
	Now           func() time.Time
}

type upgradeService struct {
	settings UpgradeSettings
}

// NewUpgradeService returns the service behind the Upgrades page.
func NewUpgradeService(settings UpgradeSettings) contracts.UpgradeService {
	if settings.Now == nil {
		settings.Now = time.Now
	}
	return &upgradeService{settings: settings}
}

func (s *upgradeService) UpgradeStatus(ctx context.Context) (contracts.UpgradeStatus, error) {
	if err := upgradeCaller(ctx); err != nil {
		return contracts.UpgradeStatus{}, err
	}
	return s.view(), nil
}

func (s *upgradeService) CheckForUpgrade(ctx context.Context) (contracts.UpgradeStatus, error) {
	if err := upgradeCaller(ctx); err != nil {
		return contracts.UpgradeStatus{}, err
	}
	if s.settings.Checker == nil {
		return contracts.UpgradeStatus{}, apierr.Invalidf("checking for new releases is turned off on this installation (METIS_UPGRADE_CHECK=false)")
	}
	last := s.settings.Checker.Status().CheckedAt
	if !last.IsZero() && s.settings.Now().Sub(last) < minCheckInterval {
		return s.view(), nil
	}
	// A failed check is shown on the page, with the previous answer kept;
	// it is not a failed request.
	_, _ = s.settings.Checker.Check(ctx)
	return s.view(), nil
}

func (s *upgradeService) view() contracts.UpgradeStatus {
	view := contracts.UpgradeStatus{
		Enabled:       s.settings.Checker != nil,
		Channel:       s.settings.Channel,
		Install:       s.settings.Install,
		SchemaVersion: s.settings.SchemaVersion,
		Reachable:     true,
	}
	if s.settings.Checker == nil {
		view.Status = releasecheck.New(releasecheck.Options{Current: s.settings.Current}).Status()
		return view
	}
	view.Status = s.settings.Checker.Status()
	if view.Newest != nil && view.Newest.Manifest != nil {
		view.Pending = view.Newest.Manifest.Pending(s.settings.SchemaVersion)
		view.Reachable = view.Newest.Manifest.Reachable(s.settings.SchemaVersion)
	}
	return view
}

// upgradeCaller is the platform administrator check every upgrade read makes,
// whatever the endpoint in front of it already asked.
func upgradeCaller(ctx context.Context) error {
	_, err := settingsCaller(ctx)
	if err != nil {
		return apierr.Forbiddenf("only an administrator of every organization can see or start an upgrade")
	}
	return nil
}

// DetectInstall says how this host runs Metis, from what the packages and the
// container runtimes leave behind.
func DetectInstall() string {
	return detectInstall(os.Getenv, fileExists, os.Executable)
}

func detectInstall(getenv func(string) string, exists func(string) bool, executable func() (string, error)) string {
	if getenv("KUBERNETES_SERVICE_HOST") != "" || exists("/.dockerenv") || exists("/run/.containerenv") {
		return contracts.InstallContainer
	}
	path, err := executable()
	if err == nil && path == "/usr/bin/metis" &&
		(exists("/usr/lib/systemd/system/metis.service") || exists("/etc/init.d/metis")) {
		return contracts.InstallPackage
	}
	return contracts.InstallBinary
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
