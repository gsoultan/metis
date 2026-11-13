// Package releasecheck finds out whether a newer release of Metis has been
// published, and what it would do to this installation's database.
//
// It reads, and does nothing else. It never downloads a package, and nothing
// it reads is ever run: installing is the upgrade helper's job, which verifies
// the release's signature itself and trusts nothing this package found. What
// leaves the host is one request to GitHub's public API for the release list
// and one for the newest release's release.json, with no installation id and
// no credentials.
package releasecheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/releasemanifest"
)

// DefaultReleasesURL is where the release list is read: the public repository
// this source tree is built from.
const DefaultReleasesURL = "https://api.github.com/repos/gsoultan/metis/releases?per_page=30"

// maxListBytes bounds the release list; thirty releases with their notes are
// well under it.
const maxListBytes = 4 << 20

// maxNotesRunes bounds the release notes kept for the page.
const maxNotesRunes = 8000

// Release is one published release, as the Upgrades page shows it.
type Release struct {
	Tag         string                    `json:"tag"`
	Name        string                    `json:"name"`
	PublishedAt time.Time                 `json:"published_at"`
	URL         string                    `json:"url"`
	Notes       string                    `json:"notes"`
	Prerelease  bool                      `json:"prerelease"`
	Manifest    *releasemanifest.Manifest `json:"manifest,omitempty"`
	// ManifestError says why release.json could not be read, when it could
	// not. A release published before manifests existed has none.
	ManifestError string `json:"manifest_error,omitempty"`
}

// Status is what the last check found.
type Status struct {
	// Current is the version this binary was linked with.
	Current string `json:"current"`
	// CurrentIsRelease is false for a build that is not from a release tag,
	// which cannot be compared with one and is never offered an upgrade.
	CurrentIsRelease bool `json:"current_is_release"`
	// Newest is the newest release later than Current, if there is one.
	Newest    *Release  `json:"newest,omitempty"`
	CheckedAt time.Time `json:"checked_at,omitzero"`
	// Error says why the last check failed. The previous answer is kept.
	Error string `json:"error,omitempty"`
}

// Options configure a Checker.
type Options struct {
	Current     string
	ReleasesURL string
	// Prerelease offers release candidates as well as final releases.
	Prerelease bool
	Client     *http.Client
	Now        func() time.Time
}

// Checker keeps the answer of the last check.
type Checker struct {
	opts Options

	mu     sync.Mutex
	status Status
	// running serialises checks: a second "Check now" while one is in flight
	// waits for its answer rather than asking GitHub twice.
	running sync.Mutex
}

// New builds a Checker. It does not check until asked.
func New(opts Options) *Checker {
	if opts.ReleasesURL == "" {
		opts.ReleasesURL = DefaultReleasesURL
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 20 * time.Second}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	_, isRelease := parseTag(opts.Current)
	return &Checker{opts: opts, status: Status{Current: opts.Current, CurrentIsRelease: isRelease}}
}

// Status is the answer of the last check, without asking again.
func (c *Checker) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// Check asks GitHub now and returns what it found. A failure is recorded on
// the status and returned; the newest release found before is kept.
func (c *Checker) Check(ctx context.Context) (Status, error) {
	c.running.Lock()
	defer c.running.Unlock()

	newest, err := c.find(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.CheckedAt = c.opts.Now().UTC()
	if err != nil {
		c.status.Error = err.Error()
		return c.status, err
	}
	c.status.Error = ""
	c.status.Newest = newest
	return c.status, nil
}

// Run checks at once and then every interval until ctx ends. A failed check
// is logged by the caller through onError and tried again at the next tick.
func (c *Checker) Run(ctx context.Context, interval time.Duration, onError func(error)) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if _, err := c.Check(ctx); err != nil && ctx.Err() == nil && onError != nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

type githubAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type githubRelease struct {
	Tag         string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	HTMLURL     string        `json:"html_url"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	PublishedAt time.Time     `json:"published_at"`
	Assets      []githubAsset `json:"assets"`
}

func (c *Checker) find(ctx context.Context) (*Release, error) {
	current, ok := parseTag(c.opts.Current)
	if !ok {
		// Nothing to compare: a laptop build is never offered a release.
		return nil, nil
	}
	var list []githubRelease
	if err := c.getJSON(ctx, c.opts.ReleasesURL, maxListBytes, &list); err != nil {
		return nil, fmt.Errorf("read the release list: %w", err)
	}

	var best *githubRelease
	var bestVersion semver
	for i := range list {
		r := &list[i]
		if r.Draft || (r.Prerelease && !c.opts.Prerelease) {
			continue
		}
		v, ok := parseTag(r.Tag)
		if !ok || (v.pre != "" && !c.opts.Prerelease) || compare(v, current) <= 0 {
			continue
		}
		if best == nil || compare(v, bestVersion) > 0 {
			best, bestVersion = r, v
		}
	}
	if best == nil {
		return nil, nil
	}

	found := &Release{
		Tag:         best.Tag,
		Name:        best.Name,
		PublishedAt: best.PublishedAt.UTC(),
		URL:         best.HTMLURL,
		Notes:       truncateRunes(best.Body, maxNotesRunes),
		Prerelease:  best.Prerelease,
	}
	manifest, err := c.manifest(ctx, best)
	if err != nil {
		found.ManifestError = err.Error()
	} else {
		found.Manifest = manifest
	}
	return found, nil
}

// manifest reads the release's release.json. The page uses it to say what
// the release does to the database; it is advice, and the helper reads and
// verifies its own copy before acting on it.
func (c *Checker) manifest(ctx context.Context, r *githubRelease) (*releasemanifest.Manifest, error) {
	for _, a := range r.Assets {
		if a.Name != "release.json" {
			continue
		}
		var raw json.RawMessage
		if err := c.getJSON(ctx, a.URL, 1<<20, &raw); err != nil {
			return nil, fmt.Errorf("read release.json: %w", err)
		}
		m, err := releasemanifest.Read(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		if m.Release != r.Tag {
			return nil, fmt.Errorf("release.json describes %s, not %s", m.Release, r.Tag)
		}
		return &m, nil
	}
	return nil, errors.New("this release publishes no release.json, so what it does to the database is not known here; read its upgrading notes")
}

func (c *Checker) getJSON(ctx context.Context, url string, limit int64, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	// GitHub refuses requests without one. Generic on purpose: nothing about
	// this installation.
	req.Header.Set("User-Agent", "metis-release-check")
	resp, err := c.opts.Client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Debug().Err(err).Msg("release check: could not close a response body")
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", req.URL.Host, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(body)) > limit {
		return fmt.Errorf("%s answered more than %d bytes", req.URL.Host, limit)
	}
	return json.Unmarshal(body, into)
}

func truncateRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
