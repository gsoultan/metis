package impl

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/releasecheck"
	"github.com/gsoultan/metis/internal/pkg/selfupgrade"
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
	// RequestPath is where a request for the helper is written, and
	// StatusPath where the helper reports; both empty off a package install.
	RequestPath string
	StatusPath  string
	// Triggered is whether something on this host acts on a request: the
	// enabled metis-upgrade.path unit.
	Triggered bool
}

// runningStaleAfter is how long a status may say "running" without a word
// before the page stops believing it: a helper killed mid-step leaves one.
const runningStaleAfter = 2 * time.Hour

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
	if _, err := s.settings.Checker.Check(ctx); err != nil {
		log.Info().Err(err).Msg("The check for a new release did not reach GitHub; the page shows the last answer")
	}
	return s.view(), nil
}

func (s *upgradeService) StartUpgrade(ctx context.Context, start contracts.UpgradeStart) (contracts.UpgradeStatus, error) {
	caller, err := settingsCaller(ctx)
	if err != nil {
		return contracts.UpgradeStatus{}, apierr.Forbiddenf("only an administrator of every organization can see or start an upgrade")
	}
	view := s.view()
	if !view.CanStart {
		return view, apierr.Invalidf("%s", view.CannotStart)
	}
	to := strings.TrimSpace(start.To)
	if to != view.Newest.Tag {
		return view, apierr.Invalidf("the release offered is %s, not %q; reload the page", view.Newest.Tag, to)
	}
	if strings.TrimSpace(start.Confirm) != to {
		return view, apierr.Invalidf("type %s to confirm the upgrade", to)
	}
	request := selfupgrade.Request{To: to, RequestedBy: caller.Username, RequestedAt: s.settings.Now().UTC()}
	if err := selfupgrade.WriteRequest(s.settings.RequestPath, request); err != nil {
		return view, apierr.Invalidf("could not ask for the upgrade: %s", err.Error())
	}
	log.Warn().Str("from", view.Current).Str("to", to).Str("by", caller.Username).
		Msg("An upgrade was asked for on the Upgrades page; metis-upgrade.service will verify and install it")
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
	s.readProgress(&view)
	view.CannotStart = s.cannotStart(view)
	view.CanStart = view.CannotStart == ""
	return view
}

// readProgress adds what the helper has left on disk. A file that cannot be
// read is left out rather than failing the page.
func (s *upgradeService) readProgress(view *contracts.UpgradeStatus) {
	if s.settings.RequestPath != "" {
		requested, err := selfupgrade.PendingRequest(s.settings.RequestPath)
		if err != nil {
			log.Warn().Err(err).Msg("The waiting upgrade request could not be read")
		}
		view.Requested = requested
	}
	if s.settings.StatusPath != "" {
		progress, err := selfupgrade.ReadStatus(s.settings.StatusPath)
		if err != nil {
			log.Warn().Err(err).Msg("The upgrade helper's status could not be read")
		}
		view.Progress = progress
	}
}

// cannotStart is why the button is not offered, or "" when it is. Each answer
// says what to do instead.
func (s *upgradeService) cannotStart(view contracts.UpgradeStatus) string {
	newest := view.Newest
	switch {
	case view.Install == contracts.InstallContainer:
		return "this installation runs from a container image; change the image tag where it is deployed"
	case view.Install != contracts.InstallPackage || s.settings.RequestPath == "":
		return "this installation is a binary placed by hand; replace it the way it was placed"
	case newest == nil:
		return "no newer release is known"
	}
	if order, ok := releasecheck.CompareTags(newest.Tag, view.Current); !ok || order <= 0 {
		return "no newer release is known"
	}
	switch {
	case newest.Manifest == nil:
		return newest.Tag + " publishes no release.json, so what it does to the database cannot be checked; upgrade it by hand following docs/upgrading.md"
	case !view.Reachable:
		return "the database is too old to go straight to " + newest.Tag + "; start " + newest.Manifest.ViaRelease + " against it first"
	case view.Requested != nil:
		return "an upgrade to " + view.Requested.To + " has already been asked for"
	case view.Progress != nil && view.Progress.Outcome == selfupgrade.OutcomeRunning &&
		s.settings.Now().Sub(view.Progress.UpdatedAt) < runningStaleAfter:
		return "an upgrade to " + view.Progress.To + " is running"
	case view.Progress != nil && view.Progress.Outcome == selfupgrade.OutcomeNeedsRestore:
		return "the last upgrade left the database for a restore; restore " + view.Progress.Backup + ", then remove the upgrade status file to offer upgrades again"
	case !s.settings.Triggered:
		return "nothing on this host acts on an upgrade request, so upgrade from a shell with `sudo metis upgrade --to " + newest.Tag + "`; on systemd, `sudo systemctl enable --now metis-upgrade.path` turns this button on"
	}
	return ""
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
