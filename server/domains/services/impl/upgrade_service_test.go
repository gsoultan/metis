package impl

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/releasecheck"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

func platformAdminContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	return context.WithValue(ctx, pkgauth.UserContextKey,
		entities.User{ID: uuid.New(), Username: "root", Roles: []string{entities.RoleAdmin}})
}

// fakeReleases answers the release list with v0.9.0 and its manifest, which
// applies migrations 55 and 56 and upgrades from schema 42.
func fakeReleases(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var listed atomic.Int32
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/releases", func(w http.ResponseWriter, _ *http.Request) {
		listed.Add(1)
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"tag_name": "v0.9.0", "html_url": "https://example.test/v0.9.0", "published_at": "2026-11-11T10:00:00Z",
			"assets": []map[string]any{{"name": "release.json", "browser_download_url": srv.URL + "/release.json"}},
		}})
	})
	mux.HandleFunc("/release.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"format":1,"release":"v0.9.0","schema_version":56,"oldest_schema":42,"via_release":"0.6.0",` +
			`"migrations":[{"version":55,"name":"person devices"},{"version":56,"name":"task reminders"}]}`))
	})
	return srv, &listed
}

func newTestUpgrades(srv *httptest.Server, schema int64, now func() time.Time) contracts.UpgradeService {
	return NewUpgradeService(UpgradeSettings{
		Checker: releasecheck.New(releasecheck.Options{
			Current: "v0.8.2", ReleasesURL: srv.URL + "/releases", Client: srv.Client(), Now: now,
		}),
		Current: "v0.8.2", Channel: "stable", Install: contracts.InstallPackage, SchemaVersion: schema, Now: now,
	})
}

func TestUpgradeStatusIsAPlatformAdministrators(t *testing.T) {
	srv, _ := fakeReleases(t)
	svc := newTestUpgrades(srv, 55, time.Now)
	tests := []struct {
		name string
		ctx  context.Context
	}{
		{"nobody signed in", t.Context()},
		{"an organization's member", context.WithValue(t.Context(), pkgauth.UserContextKey,
			entities.User{ID: uuid.New(), Username: "ana", Roles: []string{"DESIGNER"}})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.UpgradeStatus(tt.ctx); err == nil {
				t.Fatal("expected UpgradeStatus to be refused")
			}
			if _, err := svc.CheckForUpgrade(tt.ctx); err == nil {
				t.Fatal("expected CheckForUpgrade to be refused")
			}
		})
	}
}

func TestCheckForUpgradeSaysWhatTheReleaseWouldChange(t *testing.T) {
	tests := []struct {
		name      string
		schema    int64
		pending   int
		reachable bool
	}{
		{"one migration to apply", 55, 1, true},
		{"already at its schema", 56, 0, true},
		{"too old to reach directly", 36, 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := fakeReleases(t)
			status, err := newTestUpgrades(srv, tt.schema, time.Now).CheckForUpgrade(platformAdminContext(t))
			if err != nil {
				t.Fatal(err)
			}
			if status.Newest == nil || status.Newest.Tag != "v0.9.0" {
				t.Fatalf("newest = %+v", status.Newest)
			}
			if len(status.Pending) != tt.pending || status.Reachable != tt.reachable {
				t.Fatalf("pending %d reachable %v, want %d %v", len(status.Pending), status.Reachable, tt.pending, tt.reachable)
			}
			if !status.Enabled || status.Install != contracts.InstallPackage {
				t.Fatalf("status = %+v", status)
			}
		})
	}
}

func TestCheckForUpgradeAsksGitHubAtMostOnceAMinute(t *testing.T) {
	srv, listed := fakeReleases(t)
	clock := time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	svc := newTestUpgrades(srv, 55, now)
	ctx := platformAdminContext(t)

	for range 3 {
		if _, err := svc.CheckForUpgrade(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if got := listed.Load(); got != 1 {
		t.Fatalf("asked GitHub %d times within a minute, want 1", got)
	}
	clock = clock.Add(2 * time.Minute)
	if _, err := svc.CheckForUpgrade(ctx); err != nil {
		t.Fatal(err)
	}
	if got := listed.Load(); got != 2 {
		t.Fatalf("asked GitHub %d times, want 2 after a minute had passed", got)
	}
}

func TestATurnedOffCheckAsksNobody(t *testing.T) {
	svc := NewUpgradeService(UpgradeSettings{Current: "v0.8.2", Channel: "stable", Install: contracts.InstallBinary, SchemaVersion: 55})
	ctx := platformAdminContext(t)
	status, err := svc.UpgradeStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Enabled || status.Newest != nil || status.Current != "v0.8.2" || !status.CurrentIsRelease {
		t.Fatalf("status = %+v", status)
	}
	if _, err := svc.CheckForUpgrade(ctx); err == nil {
		t.Fatal("expected Check now to say the check is turned off")
	}
}

func TestDetectInstall(t *testing.T) {
	none := func(string) string { return "" }
	exe := func(path string) func() (string, error) { return func() (string, error) { return path, nil } }
	only := func(paths ...string) func(string) bool {
		return func(p string) bool {
			for _, q := range paths {
				if p == q {
					return true
				}
			}
			return false
		}
	}
	tests := []struct {
		name       string
		getenv     func(string) string
		exists     func(string) bool
		executable func() (string, error)
		want       string
	}{
		{"kubernetes", func(k string) string {
			if k == "KUBERNETES_SERVICE_HOST" {
				return "10.0.0.1"
			}
			return ""
		}, only(), exe("/metis"), contracts.InstallContainer},
		{"docker", none, only("/.dockerenv"), exe("/metis"), contracts.InstallContainer},
		{"podman", none, only("/run/.containerenv"), exe("/metis"), contracts.InstallContainer},
		{"systemd package", none, only("/usr/lib/systemd/system/metis.service"), exe("/usr/bin/metis"), contracts.InstallPackage},
		{"alpine package", none, only("/etc/init.d/metis"), exe("/usr/bin/metis"), contracts.InstallPackage},
		{"a binary somewhere else", none, only("/usr/lib/systemd/system/metis.service"), exe("/opt/metis/metis"), contracts.InstallBinary},
		{"executable unknown", none, only("/etc/init.d/metis"), func() (string, error) { return "", errors.New("no") }, contracts.InstallBinary},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectInstall(tt.getenv, tt.exists, tt.executable); got != tt.want {
				t.Fatalf("detectInstall = %q, want %q", got, tt.want)
			}
		})
	}
}
