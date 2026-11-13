package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/sigstore/sigstore-go/pkg/root"

	"github.com/gsoultan/metis/internal/pkg/selfupgrade"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

const (
	// The package's own paths; see deploy/linux and .goreleaser.yaml.
	packageEnvFile  = "/etc/metis/metis.env"
	packageStateDir = "/var/lib/metis"
	// UpgradeRequestFile is where the Upgrades page leaves a request, in the
	// service's own state directory; metis-upgrade.path watches it.
	UpgradeRequestFile = packageStateDir + "/upgrade-request.json"
	// UpgradeStatusFile is where the helper reports progress.
	UpgradeStatusFile = packageStateDir + "/upgrade/status.json"
	serviceGroup      = "metis"

	upgradeStartTimeout = 5 * time.Minute
)

// runUpgradeCommand is `metis upgrade --to vX.Y.Z`: install a newer release
// on a host where a Linux package installed this one. It runs as root, beside
// the service and never inside it, and refuses anything it cannot verify.
func (a *App) runUpgradeCommand(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	flags.SetOutput(out)
	to := flags.String("to", "", "the release to install, such as v0.8.0")
	allowDowngrade := flags.Bool("allow-downgrade", false, "install an older release than the one running")
	requestedBy := flags.String("requested-by", "", "who asked for the upgrade, kept in its status")
	envFile := flags.String("env-file", packageEnvFile, "the service's environment file")
	stateDir := flags.String("state-dir", packageStateDir, "the service's working directory")
	fromRequest := flags.String("from-request", "", "take the release and requester from the Upgrades page's request file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *fromRequest != "" {
		if *to != "" || *allowDowngrade {
			return errors.New("--from-request takes the release from the request; it is not combined with --to or --allow-downgrade")
		}
		request, err := selfupgrade.TakeRequest(*fromRequest)
		if err != nil {
			return err
		}
		*to, *requestedBy = request.To, request.RequestedBy
	}
	if *to == "" {
		return errors.New("say which release to install: metis upgrade --to vX.Y.Z (the Upgrades page names the newest)")
	}
	if os.Geteuid() != 0 {
		return errors.New("an upgrade installs a package and restarts the service, so it runs as root: sudo metis upgrade --to " + *to)
	}
	if install := serviceimpl.DetectInstall(); install != contracts.InstallPackage {
		return fmt.Errorf("this metis was not installed from a Linux package (%s), so it is upgraded the way it was installed; see docs/upgrading.md", install)
	}

	gid := 0
	if group, err := user.LookupGroup(serviceGroup); err == nil {
		gid, _ = strconv.Atoi(group.Gid)
	}
	plan, err := a.upgradePlan(ctx, *to, *envFile, *stateDir)
	if err != nil {
		if *fromRequest != "" {
			_ = selfupgrade.RecordRefusal(filepath.Join(*stateDir, "upgrade"), gid, selfupgrade.Status{
				To: *to, RequestedBy: *requestedBy, Step: "prepare", Detail: err.Error(),
			})
		}
		return err
	}
	plan.AllowDowngrade = *allowDowngrade
	plan.StatusGID = gid
	plan.RequestedBy = strings.TrimSpace(*requestedBy)
	if plan.RequestedBy == "" {
		plan.RequestedBy = os.Getenv("SUDO_USER")
	}

	fmt.Fprintf(out, "Upgrading to %s. Progress is kept in %s.\n", *to, filepath.Join(plan.Dir, "status.json"))
	status, err := plan.Run(ctx)
	for _, step := range status.Steps {
		fmt.Fprintf(out, "  %s  %-10s %s\n", step.At.Local().Format("15:04:05"), step.Name, step.Detail)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", status.Outcome, err)
	}
	fmt.Fprintf(out, "Metis %s is serving. The backup taken first is %s.\n", status.To, status.Backup)
	if status.Detail != "" {
		fmt.Fprintln(out, status.Detail)
	}
	return nil
}

// upgradePlan reads the installation the way its service reads it and builds
// the plan on the real host.
func (a *App) upgradePlan(ctx context.Context, to, envFile, stateDir string) (*selfupgrade.Plan, error) {
	if err := loadServiceEnvironment(envFile); err != nil {
		return nil, err
	}
	// config.yaml is read relative to the service's working directory.
	if err := os.Chdir(stateDir); err != nil {
		return nil, err
	}
	// The listen address the setup wizard saved there, as the service reads it.
	useConfigFile()
	dsn, err := a.resolveDSN()
	if err != nil {
		return nil, err
	}
	base, err := selfupgrade.ProbeBase(resolveAddress(envHTTPAddress, defaultHTTPAddress))
	if err != nil {
		return nil, err
	}
	format, err := selfupgrade.DetectFormat(selfupgrade.ExecProbe(ctx))
	if err != nil {
		return nil, err
	}
	// Sigstore's root of trust, fetched over TUF and checked against the root
	// sigstore-go carries.
	trusted, err := root.FetchTrustedRoot()
	if err != nil {
		return nil, fmt.Errorf("fetch Sigstore's trusted root to verify the release: %w", err)
	}
	host := selfupgrade.PackageHost{Format: format}
	database := selfupgrade.Postgres{DSN: dsn}
	return &selfupgrade.Plan{
		To:           to,
		Format:       format,
		GOARCH:       runtime.GOARCH,
		Dir:          filepath.Join(stateDir, "upgrade"),
		BackupDir:    filepath.Join(stateDir, "backups"),
		StartTimeout: upgradeStartTimeout,
		// The address is a constant in this source, so the plain client; see
		// upgradeService for why not the guarded one.
		Releases:  selfupgrade.GitHubReleases{Client: &http.Client{Timeout: 10 * time.Minute}},
		Verifier:  selfupgrade.SignatureVerifier{Trusted: trusted},
		Service:   host,
		Installer: host,
		Server:    selfupgrade.LocalServer{Client: &http.Client{Timeout: 5 * time.Second}, Base: base},
		Database:  database,
	}, nil
}

// loadServiceEnvironment sets what the service's EnvironmentFile sets, over
// anything in the caller's shell, so the upgrade reads the service's
// database and not one a stray DATABASE_URL names.
func loadServiceEnvironment(path string) error {
	raw, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	values, err := selfupgrade.ParseEnvFile(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for key, value := range values {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}
