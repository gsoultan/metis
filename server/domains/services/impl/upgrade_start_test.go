package impl

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/releasecheck"
	"github.com/gsoultan/metis/internal/pkg/selfupgrade"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// startableUpgrades is a package install with the trigger enabled, offered
// v0.9.0 from schema 55, which it reaches.
func startableUpgrades(t *testing.T, change func(*UpgradeSettings)) (contracts.UpgradeService, UpgradeSettings) {
	t.Helper()
	srv, _ := fakeReleases(t)
	dir := t.TempDir()
	settings := UpgradeSettings{
		Current: "v0.8.2", Channel: "stable", Install: contracts.InstallPackage, SchemaVersion: 55,
		RequestPath: filepath.Join(dir, "upgrade-request.json"),
		StatusPath:  filepath.Join(dir, "status.json"),
		Triggered:   true,
	}
	if change != nil {
		change(&settings)
	}
	settings.Checker = releasecheck.New(releasecheck.Options{
		Current: settings.Current, ReleasesURL: srv.URL + "/releases", Client: srv.Client(),
	})
	svc := NewUpgradeService(settings)
	if _, err := svc.CheckForUpgrade(platformAdminContext(t)); err != nil {
		t.Fatal(err)
	}
	return svc, settings
}

func TestStartUpgradeLeavesARequestForTheHelper(t *testing.T) {
	svc, settings := startableUpgrades(t, nil)
	status, err := svc.StartUpgrade(platformAdminContext(t), contracts.UpgradeStart{To: "v0.9.0", Confirm: "v0.9.0"})
	if err != nil {
		t.Fatal(err)
	}
	if status.Requested == nil || status.Requested.To != "v0.9.0" || status.Requested.RequestedBy != "root" {
		t.Fatalf("requested %+v", status.Requested)
	}
	if status.CanStart {
		t.Fatal("a second upgrade is offered while one is waiting")
	}
	request, err := selfupgrade.TakeRequest(settings.RequestPath)
	if err != nil || request.To != "v0.9.0" {
		t.Fatalf("request %+v, %v", request, err)
	}
}

func TestStartUpgradeRefuses(t *testing.T) {
	member := context.WithValue(t.Context(), pkgauth.UserContextKey,
		entities.User{ID: uuid.New(), Username: "ana", Roles: []string{"DESIGNER"}})
	cases := map[string]struct {
		ctx    func(*testing.T) context.Context
		change func(*UpgradeSettings)
		start  contracts.UpgradeStart
		want   string
	}{
		"an organization's member": {ctx: func(*testing.T) context.Context { return member }, start: contracts.UpgradeStart{To: "v0.9.0", Confirm: "v0.9.0"}, want: "only an administrator"},
		"not confirmed":            {start: contracts.UpgradeStart{To: "v0.9.0", Confirm: "v0.9"}, want: "type v0.9.0"},
		"another release":          {start: contracts.UpgradeStart{To: "v1.0.0", Confirm: "v1.0.0"}, want: "the release offered is v0.9.0"},
		"a container":              {change: func(s *UpgradeSettings) { s.Install = contracts.InstallContainer }, start: contracts.UpgradeStart{To: "v0.9.0", Confirm: "v0.9.0"}, want: "container image"},
		"a binary":                 {change: func(s *UpgradeSettings) { s.Install = contracts.InstallBinary }, start: contracts.UpgradeStart{To: "v0.9.0", Confirm: "v0.9.0"}, want: "binary placed by hand"},
		"no trigger":               {change: func(s *UpgradeSettings) { s.Triggered = false }, start: contracts.UpgradeStart{To: "v0.9.0", Confirm: "v0.9.0"}, want: "metis-upgrade.path"},
		"database too old":         {change: func(s *UpgradeSettings) { s.SchemaVersion = 30 }, start: contracts.UpgradeStart{To: "v0.9.0", Confirm: "v0.9.0"}, want: "too old"},
		"already running":          {change: func(s *UpgradeSettings) { s.Current = "v0.9.0" }, start: contracts.UpgradeStart{To: "v0.9.0", Confirm: "v0.9.0"}, want: "no newer release"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			svc, settings := startableUpgrades(t, c.change)
			ctx := platformAdminContext(t)
			if c.ctx != nil {
				ctx = c.ctx(t)
			}
			_, err := svc.StartUpgrade(ctx, c.start)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want %q", err, c.want)
			}
			if _, statErr := os.Stat(settings.RequestPath); !os.IsNotExist(statErr) {
				t.Fatal("a refused start left a request")
			}
		})
	}
}

func TestARunningUpgradeIsShownAndBlocksAnother(t *testing.T) {
	cases := map[string]struct {
		status    selfupgrade.Status
		canStart  bool
		cannotSay string
	}{
		"running":         {selfupgrade.Status{To: "v0.9.0", Outcome: selfupgrade.OutcomeRunning, UpdatedAt: time.Now().UTC()}, false, "is running"},
		"running, silent": {selfupgrade.Status{To: "v0.9.0", Outcome: selfupgrade.OutcomeRunning, UpdatedAt: time.Now().Add(-3 * time.Hour).UTC()}, true, ""},
		"needs a restore": {selfupgrade.Status{To: "v0.9.0", Outcome: selfupgrade.OutcomeNeedsRestore, Backup: "/b.dump", UpdatedAt: time.Now().UTC()}, false, "restore /b.dump"},
		"rolled back":     {selfupgrade.Status{To: "v0.9.0", Outcome: selfupgrade.OutcomeRolledBack, UpdatedAt: time.Now().UTC()}, true, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			svc, settings := startableUpgrades(t, nil)
			raw, _ := json.Marshal(c.status)
			if err := os.WriteFile(settings.StatusPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			status, err := svc.UpgradeStatus(platformAdminContext(t))
			if err != nil {
				t.Fatal(err)
			}
			if status.Progress == nil || status.Progress.Outcome != c.status.Outcome {
				t.Fatalf("progress %+v", status.Progress)
			}
			if status.CanStart != c.canStart || !strings.Contains(status.CannotStart, c.cannotSay) {
				t.Fatalf("can start %v (%q)", status.CanStart, status.CannotStart)
			}
		})
	}
}
