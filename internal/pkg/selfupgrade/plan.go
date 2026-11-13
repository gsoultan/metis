// Package selfupgrade installs a newer release of Metis on a host where a
// Linux package installed it, and refuses to run anything it cannot verify.
//
// It runs as root in its own process, never inside the server: the server is
// confined by its systemd unit and cannot write /usr/bin, and that stays so.
// The only thing it takes from the server is a version to go to. Everything
// else it fetches and checks itself: the signature over checksums.txt against
// the identity pinned in this binary, each file against checksums.txt, the
// release manifest, a backup that completed, and the new version answering
// its probes. A step that fails before the service is stopped changes nothing.
package selfupgrade

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gsoultan/metis/internal/pkg/releasecheck"
	"github.com/gsoultan/metis/internal/pkg/releasemanifest"
)

// Plan is one upgrade to one release.
type Plan struct {
	To             string
	RequestedBy    string
	AllowDowngrade bool
	Format         Format
	GOARCH         string
	// Dir holds downloads, the previous package, status.json and the lock.
	Dir string
	// BackupDir is where the pre-upgrade backup is written.
	BackupDir string
	// StartTimeout bounds how long the new release has to answer /readyz.
	StartTimeout time.Duration
	PollEvery    time.Duration

	Releases  Releases
	Verifier  Verifier
	Service   Service
	Installer Installer
	Server    Server
	Database  Database
	Now       func() time.Time
}

// verified is a release whose files have all been checked.
type verified struct {
	manifest releasemanifest.Manifest
	pkgPath  string
}

// Run carries the plan out and returns the final status. The error is
// non-nil for every outcome but OutcomeDone.
func (p *Plan) Run(ctx context.Context) (Status, error) {
	r := &run{plan: p, status: Status{To: p.To, RequestedBy: p.RequestedBy, Outcome: OutcomeRunning, StartedAt: p.now()}}
	if err := os.MkdirAll(p.Dir, 0o750); err != nil {
		return r.status, err
	}
	unlock, err := lock(filepath.Join(p.Dir, "lock"))
	if err != nil {
		return r.status, err
	}
	defer unlock()
	return r.execute(ctx)
}

func (p *Plan) now() time.Time {
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

type run struct {
	plan   *Plan
	status Status
}

func (r *run) execute(ctx context.Context) (Status, error) {
	p := r.plan
	from, err := p.Server.Version(ctx)
	if err != nil {
		return r.refuse("check the running version", fmt.Errorf("the running metis does not answer /healthz, so there is nothing known to upgrade from: %w", err))
	}
	r.status.From = from
	if err := r.checkDirection(from); err != nil {
		return r.refuse("check the versions", err)
	}

	target, err := r.fetchVerified(ctx, p.To)
	if err != nil {
		return r.refuse("verify "+p.To, err)
	}
	schemaBefore, err := p.Database.SchemaVersion(ctx)
	if err != nil {
		return r.refuse("read the database", err)
	}
	if !target.manifest.Reachable(schemaBefore) {
		return r.refuse("check the upgrade path", fmt.Errorf(
			"the database is at schema %d and %s upgrades from %d or later; start Metis %s against it once first",
			schemaBefore, p.To, target.manifest.OldestSchema, target.manifest.ViaRelease))
	}
	// The release to roll back to, fetched and verified before anything is
	// stopped, so a rollback never depends on the network.
	previous, err := r.fetchVerified(ctx, from)
	if err != nil {
		return r.refuse("verify "+from+" for a rollback", err)
	}
	r.step("verified", fmt.Sprintf("%s and %s are signed by the metis release workflow and match their checksums", p.To, from))

	instancesBefore, err := p.Database.Instances(ctx)
	if err != nil {
		return r.refuse("count instances", err)
	}
	backup := filepath.Join(p.BackupDir, fmt.Sprintf("%s-pre-%s.dump", p.now().Format("20060102T150405Z"), p.To))
	size, err := p.Database.Backup(ctx, backup)
	if err != nil || size <= 0 {
		return r.refuse("back up", errors.Join(errors.New("the backup did not complete, so nothing was changed"), err))
	}
	r.status.Backup = backup
	r.step("backed up", fmt.Sprintf("%s, %d bytes", backup, size))

	// From here the host changes.
	if err := p.Service.Stop(ctx); err != nil {
		return r.refuse("stop metis", err)
	}
	r.step("stopped", "")
	if err := p.Installer.Install(ctx, target.pkgPath); err != nil {
		return r.recover(ctx, previous, schemaBefore, fmt.Errorf("install %s: %w", p.To, err))
	}
	r.step("installed", filepath.Base(target.pkgPath))
	if err := p.Service.Start(ctx); err != nil {
		return r.recover(ctx, previous, schemaBefore, fmt.Errorf("start %s: %w", p.To, err))
	}
	if err := r.waitFor(ctx, p.To); err != nil {
		return r.recover(ctx, previous, schemaBefore, err)
	}
	r.step("started", p.To+" answers its probes")

	instancesAfter, err := p.Database.Instances(ctx)
	switch {
	case err != nil:
		r.status.Detail = "the upgrade finished but the instances could not be counted again: " + err.Error()
	case instancesAfter < instancesBefore:
		r.status.Detail = fmt.Sprintf("the upgrade finished but %d instances were counted before and %d after; look before anybody works on", instancesBefore, instancesAfter)
	default:
		r.step("counted", fmt.Sprintf("%d instances before, %d after", instancesBefore, instancesAfter))
	}
	return r.finish(OutcomeDone, "done", nil)
}

// checkDirection refuses anything but a move forward between two releases,
// unless a downgrade was asked for by name.
func (r *run) checkDirection(from string) error {
	order, ok := releasecheck.CompareTags(r.plan.To, from)
	switch {
	case !ok:
		return fmt.Errorf("%s and %s are not both release tags; only a release is upgraded from or to", from, r.plan.To)
	case order == 0:
		return fmt.Errorf("%s is already running", from)
	case order < 0 && !r.plan.AllowDowngrade:
		return fmt.Errorf("%s is older than the running %s; a downgrade is only done by hand, with --allow-downgrade", r.plan.To, from)
	}
	return nil
}

// fetchVerified downloads a release's signed checksums, its manifest and the
// package for this host, and checks every one.
func (r *run) fetchVerified(ctx context.Context, tag string) (verified, error) {
	p := r.plan
	checksumsRaw, err := p.Releases.Asset(ctx, tag, "checksums.txt")
	if err != nil {
		return verified{}, err
	}
	bundle, err := p.Releases.Asset(ctx, tag, "checksums.txt.sigstore.json")
	if err != nil {
		return verified{}, fmt.Errorf("%s publishes no signature for its checksums, so it cannot be verified: %w", tag, err)
	}
	if err := p.Verifier.VerifyChecksums(checksumsRaw, bundle, tag); err != nil {
		return verified{}, err
	}
	sums, err := ParseChecksums(checksumsRaw)
	if err != nil {
		return verified{}, err
	}
	manifestRaw, err := p.Releases.Asset(ctx, tag, "release.json")
	if err != nil {
		return verified{}, err
	}
	if err := sums.Check("release.json", manifestRaw); err != nil {
		return verified{}, err
	}
	manifest, err := releasemanifest.Read(bytes.NewReader(manifestRaw))
	if err != nil {
		return verified{}, err
	}
	if manifest.Release != tag {
		return verified{}, fmt.Errorf("%w: release.json describes %s, not %s", ErrNotVerified, manifest.Release, tag)
	}
	name, err := PackageName(p.Format, tag, p.GOARCH)
	if err != nil {
		return verified{}, err
	}
	pkg, err := p.Releases.Asset(ctx, tag, name)
	if err != nil {
		return verified{}, err
	}
	if err := sums.Check(name, pkg); err != nil {
		return verified{}, err
	}
	dir := filepath.Join(p.Dir, tag)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return verified{}, err
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, pkg, 0o600); err != nil {
		return verified{}, err
	}
	return verified{manifest: manifest, pkgPath: path}, nil
}
