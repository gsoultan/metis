package selfupgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/gsoultan/metis/internal/pkg/releasecheck"
)

// The real host: GitHub for downloads, the package manager, the init system,
// the server's own probes and PostgreSQL.

// DownloadBase is where GoReleaser publishes a release's files.
const DownloadBase = "https://github.com/gsoultan/metis/releases/download/"

// maxAsset bounds one download. A package is tens of megabytes; anything near
// this is not one of ours.
const maxAsset = 512 << 20

// GitHubReleases downloads release files over HTTPS.
type GitHubReleases struct {
	Client *http.Client
	// Base defaults to DownloadBase; a test points it elsewhere.
	Base string
}

func (g GitHubReleases) Asset(ctx context.Context, tag, name string) ([]byte, error) {
	// The tag goes into a URL path, so only a release tag is accepted.
	if _, ok := releasecheck.CompareTags(tag, tag); !ok || strings.ContainsAny(name, "/\\?#") {
		return nil, fmt.Errorf("%q / %q is not a release file", tag, name)
	}
	base := g.Base
	if base == "" {
		base = DownloadBase
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+url.PathEscape(tag)+"/"+url.PathEscape(name), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "metis-upgrade")
	resp, err := g.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s of %s: %w", name, tag, err)
	}
	defer closing(resp.Body, "a download")
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s of %s: %s", name, tag, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAsset+1))
	if err != nil {
		return nil, fmt.Errorf("download %s of %s: %w", name, tag, err)
	}
	if len(body) > maxAsset {
		return nil, fmt.Errorf("%s of %s is larger than any metis release file", name, tag)
	}
	return body, nil
}

// ExecProbe asks the package manager's own database whether metis is installed.
func ExecProbe(ctx context.Context) Probe {
	return func(f Format) bool {
		name, args := QueryCommand(f)
		if _, err := exec.LookPath(name); err != nil {
			return false
		}
		return exec.CommandContext(ctx, name, args...).Run() == nil // #nosec G204 -- fixed commands from QueryCommand
	}
}

// PackageHost installs with the package manager and drives the service with
// the init system its format ships a unit for: OpenRC on Alpine, systemd
// everywhere else.
type PackageHost struct {
	Format Format
}

func (h PackageHost) Install(ctx context.Context, path string) error {
	name, args, err := InstallCommand(h.Format, path)
	if err != nil {
		return err
	}
	return runCommand(ctx, name, args...)
}

func (h PackageHost) Stop(ctx context.Context) error { return h.service(ctx, "stop") }

func (h PackageHost) Start(ctx context.Context) error { return h.service(ctx, "start") }

func (h PackageHost) service(ctx context.Context, verb string) error {
	if h.Format == FormatAPK {
		return runCommand(ctx, "rc-service", "metis", verb)
	}
	return runCommand(ctx, "systemctl", verb, "metis.service")
}

func runCommand(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput() // #nosec G204 -- the command and its arguments are built in this package
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if len(detail) > 2000 {
			detail = detail[len(detail)-2000:]
		}
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, detail)
	}
	return nil
}

// LocalServer asks the server on this host over its health probes.
type LocalServer struct {
	Client *http.Client
	// Base is http://host:port, from ProbeBase.
	Base string
}

// ProbeBase turns the address the server listens on into one to call it on:
// a wildcard listener is reached on loopback.
func ProbeBase(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("METIS_HTTP_ADDRESS %q is not host:port: %w", listen, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

func (s LocalServer) Version(ctx context.Context) (string, error) {
	resp, err := s.get(ctx, "/healthz")
	if err != nil {
		return "", err
	}
	defer closing(resp.Body, "/healthz")
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("/healthz answered %s", resp.Status)
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil {
		return "", fmt.Errorf("/healthz: %w", err)
	}
	if body.Version == "" {
		return "", errors.New("/healthz does not say which version is running")
	}
	return body.Version, nil
}

func (s LocalServer) Ready(ctx context.Context) bool {
	resp, err := s.get(ctx, "/readyz")
	if err != nil {
		return false
	}
	closing(resp.Body, "/readyz")
	return resp.StatusCode == http.StatusOK
}

func (s LocalServer) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Base+path, nil)
	if err != nil {
		return nil, err
	}
	return s.Client.Do(req)
}

// Postgres is the installation's database.
type Postgres struct {
	DSN string
}

func (p Postgres) SchemaVersion(ctx context.Context) (int64, error) {
	var v int64
	err := p.query(ctx, func(c *pgx.Conn) error {
		return c.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v)
	})
	return v, err
}

// Instances counts every row, deleted or archived, so any that went missing
// across the restart show.
func (p Postgres) Instances(ctx context.Context) (int64, error) {
	var n int64
	err := p.query(ctx, func(c *pgx.Conn) error {
		return c.QueryRow(ctx, `SELECT (SELECT COUNT(*) FROM process_instances) + (SELECT COUNT(*) FROM archived_instances)`).Scan(&n)
	})
	return n, err
}

func (p Postgres) query(ctx context.Context, fn func(*pgx.Conn) error) error {
	conn, err := pgx.Connect(ctx, p.DSN)
	if err != nil {
		return fmt.Errorf("connect to the database: %w", err)
	}
	// A read that worked and a connection that would not close is still a
	// failed read: the upgrade acts on nothing it is unsure of.
	return errors.Join(fn(conn), conn.Close(ctx))
}

// Backup runs pg_dump in its custom format, which pg_restore reads. The
// connection goes to pg_dump in its environment, never on its command line,
// where every user on the host could read the password.
func (p Postgres) Backup(ctx context.Context, path string) (int64, error) {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		return 0, errors.New("pg_dump is not installed; install the PostgreSQL client tools so the upgrade can take a backup first")
	}
	env, err := libpqEnvironment(p.DSN)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return 0, err
	}
	cmd := exec.CommandContext(ctx, "pg_dump", "--format=custom", "--file="+path) // #nosec G204 -- the path is built by the plan
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		removing(path)
		return 0, fmt.Errorf("pg_dump: %w: %s", err, strings.TrimSpace(string(out)))
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// libpqEnvironment is a connection string as libpq's PG* variables.
func libpqEnvironment(dsn string) ([]string, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("the database connection string does not parse: %w", err)
	}
	env := []string{
		"PGHOST=" + cfg.Host,
		fmt.Sprintf("PGPORT=%d", cfg.Port),
		"PGUSER=" + cfg.User,
		"PGPASSWORD=" + cfg.Password,
		"PGDATABASE=" + cfg.Database,
	}
	if mode := sslMode(dsn); mode != "" {
		env = append(env, "PGSSLMODE="+mode)
	}
	return env, nil
}

// sslMode is the sslmode a connection string names, in either of its forms.
func sslMode(dsn string) string {
	if u, err := url.Parse(dsn); err == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql") {
		return u.Query().Get("sslmode")
	}
	for _, field := range strings.Fields(dsn) {
		if value, ok := strings.CutPrefix(field, "sslmode="); ok {
			return strings.Trim(value, `'"`)
		}
	}
	return ""
}
