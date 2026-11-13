package selfupgrade

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gsoultan/metis/internal/pkg/releasemanifest"
)

// release is one published release, as the fake download site holds it.
type release struct {
	files map[string][]byte
}

func publish(t *testing.T, tag string, schema int64) release {
	t.Helper()
	m, err := releasemanifest.Build(releasemanifest.Inputs{
		Release: tag, Converted: 50, OldestSchema: 42, ViaRelease: "v0.6.0",
		Migrations: []releasemanifest.Migration{{Version: schema, Name: "step"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var manifest bytes.Buffer
	if err := releasemanifest.Write(&manifest, m); err != nil {
		t.Fatal(err)
	}
	pkgName, err := PackageName(FormatDeb, tag, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"release.json": manifest.Bytes(),
		pkgName:        []byte("package " + tag),
	}
	var sums strings.Builder
	for name, content := range files {
		digest := sha256.Sum256(content)
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(digest[:]), name)
	}
	files["checksums.txt"] = []byte(sums.String())
	files["checksums.txt.sigstore.json"] = []byte("signed " + tag)
	return release{files: files}
}

type fakeSite map[string]release

func (s fakeSite) Asset(_ context.Context, tag, name string) ([]byte, error) {
	content, ok := s[tag].files[name]
	if !ok {
		return nil, fmt.Errorf("%s has no %s", tag, name)
	}
	return content, nil
}

// fakeVerifier accepts a bundle that names the tag, as the real one accepts
// only a signature for that tag.
type fakeVerifier struct{}

func (fakeVerifier) VerifyChecksums(_, bundle []byte, tag string) error {
	if string(bundle) != "signed "+tag {
		return ErrNotVerified
	}
	return nil
}

// fakeHost is the machine: what is installed, what is running and the
// database it runs against.
type fakeHost struct {
	installed, running string
	schema             int64
	// migratesTo is the schema each version moves the database to on start.
	migratesTo map[string]int64
	// broken versions start but never answer ready.
	broken      map[string]bool
	installFail map[string]bool
	backupFail  bool
	instances   int64
	calls       []string
}

func (h *fakeHost) Stop(context.Context) error {
	h.calls = append(h.calls, "stop")
	h.running = ""
	return nil
}

func (h *fakeHost) Start(context.Context) error {
	h.calls = append(h.calls, "start "+h.installed)
	h.running = h.installed
	if to, ok := h.migratesTo[h.installed]; ok && to > h.schema {
		h.schema = to
	}
	return nil
}

func (h *fakeHost) Install(_ context.Context, path string) error {
	version := strings.TrimPrefix(filepath.Base(filepath.Dir(path)), "")
	h.calls = append(h.calls, "install "+version)
	if h.installFail[version] {
		return errors.New("dpkg failed")
	}
	h.installed = version
	return nil
}

func (h *fakeHost) Version(context.Context) (string, error) {
	if h.running == "" {
		return "", errors.New("connection refused")
	}
	return h.running, nil
}

func (h *fakeHost) Ready(context.Context) bool { return h.running != "" && !h.broken[h.running] }

func (h *fakeHost) SchemaVersion(context.Context) (int64, error) { return h.schema, nil }
func (h *fakeHost) Instances(context.Context) (int64, error)     { return h.instances, nil }

func (h *fakeHost) Backup(context.Context, string) (int64, error) {
	h.calls = append(h.calls, "backup")
	if h.backupFail {
		return 0, errors.New("pg_dump: connection refused")
	}
	return 1024, nil
}

func newPlan(t *testing.T, host *fakeHost, site fakeSite, to string) *Plan {
	t.Helper()
	dir := t.TempDir()
	return &Plan{
		To: to, RequestedBy: "admin", Format: FormatDeb, GOARCH: "amd64",
		Dir: filepath.Join(dir, "upgrade"), BackupDir: filepath.Join(dir, "backups"),
		StartTimeout: 50 * time.Millisecond, PollEvery: time.Millisecond,
		Releases: site, Verifier: fakeVerifier{}, Service: host, Installer: host, Server: host, Database: host,
	}
}

func standardSite(t *testing.T) fakeSite {
	return fakeSite{"v0.7.3": publish(t, "v0.7.3", 55), "v0.8.0": publish(t, "v0.8.0", 57)}
}

func runningHost() *fakeHost {
	return &fakeHost{installed: "v0.7.3", running: "v0.7.3", schema: 55, instances: 12,
		migratesTo: map[string]int64{"v0.8.0": 57}, broken: map[string]bool{}, installFail: map[string]bool{}}
}

func TestAnUpgradeThatComesUpIsDone(t *testing.T) {
	host := runningHost()
	plan := newPlan(t, host, standardSite(t), "v0.8.0")
	status, err := plan.Run(t.Context())
	if err != nil || status.Outcome != OutcomeDone {
		t.Fatalf("outcome %s, err %v", status.Outcome, err)
	}
	if host.running != "v0.8.0" || status.From != "v0.7.3" || status.Backup == "" {
		t.Fatalf("running %s, status %+v", host.running, status)
	}
	want := "backup,stop,install v0.8.0,start v0.8.0"
	if got := strings.Join(host.calls, ","); got != want {
		t.Fatalf("calls %s, want %s", got, want)
	}
	saved, err := ReadStatus(filepath.Join(plan.Dir, "status.json"))
	if err != nil || saved == nil || saved.Outcome != OutcomeDone {
		t.Fatalf("status.json %+v, %v", saved, err)
	}
}

// Every refusal must leave the host as it was: nothing stopped, nothing
// installed, nothing backed up past what was asked.
func TestRefusalsChangeNothing(t *testing.T) {
	cases := map[string]struct {
		to     string
		change func(*fakeHost, fakeSite)
		want   string
	}{
		"same version":    {to: "v0.7.3", want: "already running"},
		"downgrade":       {to: "v0.7.2", want: "older than"},
		"not a release":   {to: "main", want: "not both release tags"},
		"unsigned target": {to: "v0.8.0", change: func(_ *fakeHost, s fakeSite) { delete(s["v0.8.0"].files, "checksums.txt.sigstore.json") }, want: "publishes no signature"},
		"signature for other": {to: "v0.8.0", change: func(_ *fakeHost, s fakeSite) {
			s["v0.8.0"].files["checksums.txt.sigstore.json"] = []byte("signed v0.7.3")
		}, want: "not verified"},
		"tampered package": {to: "v0.8.0", change: func(_ *fakeHost, s fakeSite) {
			s["v0.8.0"].files["metis_0.8.0_amd64.deb"] = []byte("something else")
		}, want: "does not match"},
		"manifest of another release": {to: "v0.8.0", change: func(t *fakeHost, s fakeSite) {
			// A genuine release.json from another release, listed correctly in
			// this release's checksums, still names the wrong tag.
			other := s["v0.7.3"].files["release.json"]
			old := sha256Of(s, "v0.8.0")
			s["v0.8.0"].files["release.json"] = other
			sum := sha256.Sum256(other)
			s["v0.8.0"].files["checksums.txt"] = []byte(strings.Replace(string(s["v0.8.0"].files["checksums.txt"]),
				hex.EncodeToString(old), hex.EncodeToString(sum[:]), 1))
		}, want: "describes v0.7.3"},
		"schema too old":    {to: "v0.8.0", change: func(h *fakeHost, _ fakeSite) { h.schema = 30 }, want: "start Metis v0.6.0"},
		"no rollback":       {to: "v0.8.0", change: func(_ *fakeHost, s fakeSite) { delete(s, "v0.7.3") }, want: "for a rollback"},
		"backup failed":     {to: "v0.8.0", change: func(h *fakeHost, _ fakeSite) { h.backupFail = true }, want: "backup did not complete"},
		"server not answer": {to: "v0.8.0", change: func(h *fakeHost, _ fakeSite) { h.running = "" }, want: "does not answer"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			host, site := runningHost(), standardSite(t)
			if c.change != nil {
				c.change(host, site)
			}
			before := host.running
			status, err := newPlan(t, host, site, c.to).Run(t.Context())
			if err == nil || status.Outcome != OutcomeRefused {
				t.Fatalf("outcome %s, err %v", status.Outcome, err)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(c.want)) {
				t.Fatalf("error %q does not say %q", err, c.want)
			}
			for _, call := range host.calls {
				if call != "backup" {
					t.Fatalf("a refused upgrade touched the host: %v", host.calls)
				}
			}
			if host.running != before {
				t.Fatalf("running changed from %q to %q", before, host.running)
			}
		})
	}
}

func sha256Of(s fakeSite, tag string) []byte {
	sum := sha256.Sum256(s[tag].files["release.json"])
	return sum[:]
}

func TestADowngradeAskedForByNameIsAllowed(t *testing.T) {
	host := runningHost()
	host.installed, host.running = "v0.8.0", "v0.8.0"
	plan := newPlan(t, host, standardSite(t), "v0.7.3")
	plan.AllowDowngrade = true
	// The schema has not moved past what v0.7.3 reads in this fake, so the
	// only question is whether the direction check lets it through.
	status, err := plan.Run(t.Context())
	if err != nil || status.Outcome != OutcomeDone {
		t.Fatalf("outcome %s, err %v", status.Outcome, err)
	}
}

func TestAFailureBeforeMigratingRollsBack(t *testing.T) {
	host := runningHost()
	host.migratesTo = map[string]int64{} // v0.8.0 dies before it migrates
	host.broken["v0.8.0"] = true
	status, err := newPlan(t, host, standardSite(t), "v0.8.0").Run(t.Context())
	if err == nil || status.Outcome != OutcomeRolledBack {
		t.Fatalf("outcome %s, err %v", status.Outcome, err)
	}
	if host.running != "v0.7.3" || host.installed != "v0.7.3" {
		t.Fatalf("running %s, installed %s", host.running, host.installed)
	}
}

func TestAFailedInstallRollsBack(t *testing.T) {
	host := runningHost()
	host.installFail["v0.8.0"] = true
	status, err := newPlan(t, host, standardSite(t), "v0.8.0").Run(t.Context())
	if err == nil || status.Outcome != OutcomeRolledBack || host.running != "v0.7.3" {
		t.Fatalf("outcome %s, running %s, err %v", status.Outcome, host.running, err)
	}
}

// After a migration the previous release cannot read the database. Starting
// it would be worse than stopping, so metis stays stopped and the backup is
// named.
func TestAFailureAfterMigratingStopsAndNamesTheBackup(t *testing.T) {
	host := runningHost()
	host.broken["v0.8.0"] = true
	status, err := newPlan(t, host, standardSite(t), "v0.8.0").Run(t.Context())
	if err == nil || status.Outcome != OutcomeNeedsRestore {
		t.Fatalf("outcome %s, err %v", status.Outcome, err)
	}
	if host.running != "" {
		t.Fatalf("%s is running against a migrated database", host.running)
	}
	if !strings.Contains(err.Error(), status.Backup) {
		t.Fatalf("error %q does not name the backup %s", err, status.Backup)
	}
	for _, call := range host.calls {
		if call == "install v0.7.3" {
			t.Fatal("the previous release was reinstalled over a migrated database")
		}
	}
}

func TestLostInstancesAreReported(t *testing.T) {
	host := runningHost()
	site := standardSite(t)
	plan := newPlan(t, host, site, "v0.8.0")
	plan.Database = &shrinking{fakeHost: host}
	status, err := plan.Run(t.Context())
	if err != nil || status.Outcome != OutcomeDone || !strings.Contains(status.Detail, "12 instances were counted before and 11 after") {
		t.Fatalf("outcome %s, detail %q, err %v", status.Outcome, status.Detail, err)
	}
}

type shrinking struct {
	*fakeHost
	counted int
}

func (s *shrinking) Instances(context.Context) (int64, error) {
	s.counted++
	if s.counted > 1 {
		return 11, nil
	}
	return 12, nil
}

func TestTwoUpgradesNeverOverlap(t *testing.T) {
	dir := t.TempDir()
	unlock, err := lock(filepath.Join(dir, "lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := lock(filepath.Join(dir, "lock")); err == nil {
		t.Fatal("a second lock was taken while the first was held")
	}
}
