package releasecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRelease struct {
	tag        string
	prerelease bool
	draft      bool
	manifest   string // "" means no release.json asset
}

func fakeGitHub(t *testing.T, releases []fakeRelease) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("User-Agent") == "" {
			http.Error(w, "no user agent", http.StatusForbidden)
			return
		}
		var out []map[string]any
		for _, rel := range releases {
			assets := []map[string]any{{"name": "checksums.txt", "browser_download_url": srv.URL + "/dl/" + rel.tag + "/checksums.txt"}}
			if rel.manifest != "" {
				assets = append(assets, map[string]any{"name": "release.json", "browser_download_url": srv.URL + "/dl/" + rel.tag + "/release.json"})
			}
			out = append(out, map[string]any{
				"tag_name": rel.tag, "name": rel.tag, "body": "Notes for " + rel.tag,
				"html_url": "https://github.com/gsoultan/metis/releases/tag/" + rel.tag,
				"draft":    rel.draft, "prerelease": rel.prerelease,
				"published_at": "2026-11-11T10:00:00Z", "assets": assets,
			})
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/dl/{tag}/release.json", func(w http.ResponseWriter, r *http.Request) {
		for _, rel := range releases {
			if rel.tag == r.PathValue("tag") {
				_, _ = w.Write([]byte(rel.manifest))
				return
			}
		}
		http.NotFound(w, r)
	})
	return srv, &calls
}

func manifestFor(tag string) string {
	return fmt.Sprintf(`{"format":1,"release":%q,"schema_version":56,"oldest_schema":42,"via_release":"0.6.0","migrations":[{"version":55,"name":"person devices"},{"version":56,"name":"task reminders"}]}`, tag)
}

func newChecker(srv *httptest.Server, current string, pre bool) *Checker {
	return New(Options{
		Current:     current,
		ReleasesURL: srv.URL + "/releases",
		Prerelease:  pre,
		Client:      srv.Client(),
		Now:         func() time.Time { return time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC) },
	})
}

func TestCheckFindsTheNewestLaterRelease(t *testing.T) {
	srv, _ := fakeGitHub(t, []fakeRelease{
		{tag: "v0.8.2"},
		{tag: "v0.9.0", manifest: manifestFor("v0.9.0")},
		{tag: "v0.10.0-rc.1", prerelease: true, manifest: manifestFor("v0.10.0-rc.1")},
		{tag: "v0.11.0", draft: true},
		{tag: "nightly"},
	})
	tests := []struct {
		name    string
		current string
		pre     bool
		want    string
	}{
		{"final releases only", "v0.8.2", false, "v0.9.0"},
		{"release candidates when asked", "v0.8.2", true, "v0.10.0-rc.1"},
		{"already newest", "v0.9.0", false, ""},
		{"a laptop build is never offered one", "dev", false, ""},
		{"nor a build past a tag", "v0.8.2-4-gabc1234", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			status, err := newChecker(srv, tt.current, tt.pre).Check(ctx)
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if status.Newest != nil {
				got = status.Newest.Tag
			}
			if got != tt.want {
				t.Fatalf("newest = %q, want %q", got, tt.want)
			}
			if status.CheckedAt.IsZero() {
				t.Fatal("CheckedAt not set")
			}
		})
	}
}

func TestCheckReadsTheManifest(t *testing.T) {
	srv, _ := fakeGitHub(t, []fakeRelease{{tag: "v0.9.0", manifest: manifestFor("v0.9.0")}})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	status, err := newChecker(srv, "v0.8.2", false).Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m := status.Newest.Manifest
	if m == nil || m.SchemaVersion != 56 || len(m.Pending(55)) != 1 {
		t.Fatalf("manifest = %+v, error %q", m, status.Newest.ManifestError)
	}
}

func TestCheckSaysWhenTheManifestCannotBeTrusted(t *testing.T) {
	tests := []struct {
		name, manifest, want string
	}{
		{"none published", "", "publishes no release.json"},
		{"another release's", manifestFor("v0.8.0"), "describes v0.8.0"},
		{"unreadable", "<html>", "read release.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := fakeGitHub(t, []fakeRelease{{tag: "v0.9.0", manifest: tt.manifest}})
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			status, err := newChecker(srv, "v0.8.2", false).Check(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if status.Newest == nil || status.Newest.Manifest != nil || !strings.Contains(status.Newest.ManifestError, tt.want) {
				t.Fatalf("got %+v, want a manifest error mentioning %q", status.Newest, tt.want)
			}
		})
	}
}

func TestAFailedCheckKeepsTheLastAnswer(t *testing.T) {
	srv, _ := fakeGitHub(t, []fakeRelease{{tag: "v0.9.0", manifest: manifestFor("v0.9.0")}})
	c := newChecker(srv, "v0.8.2", false)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := c.Check(ctx); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	status, err := c.Check(ctx)
	if err == nil {
		t.Fatal("expected the check to fail once GitHub is gone")
	}
	if status.Newest == nil || status.Newest.Tag != "v0.9.0" || status.Error == "" {
		t.Fatalf("status = %+v, want the earlier answer kept and the error said", status)
	}
}

func TestRunChecksAtOnceAndStops(t *testing.T) {
	srv, calls := fakeGitHub(t, nil)
	c := newChecker(srv, "v0.8.2", false)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	done := make(chan struct{})
	go func() { c.Run(ctx, time.Hour, nil); close(done) }()
	deadline := time.After(3 * time.Second)
	for calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("Run did not check at once")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop when its context ended")
	}
}
