package selfupgrade

import "context"

// The host, as the upgrade sees it. Each is an interface so the order of the
// steps and every way out of them can be tested without a package manager,
// a database or a network.

// Releases downloads a published release's files by name.
type Releases interface {
	Asset(ctx context.Context, tag, name string) ([]byte, error)
}

// Verifier checks checksums.txt against its signature bundle.
type Verifier interface {
	VerifyChecksums(checksums, bundle []byte, tag string) error
}

// Service starts and stops the metis service through the init system.
type Service interface {
	Stop(ctx context.Context) error
	Start(ctx context.Context) error
}

// Installer installs a package file with the host's package manager.
type Installer interface {
	Install(ctx context.Context, path string) error
}

// Server is the running metis, asked over its health probes.
type Server interface {
	// Version is what /healthz reports, or an error when nothing answers.
	Version(ctx context.Context) (string, error)
	// Ready is whether /readyz answers 200.
	Ready(ctx context.Context) bool
}

// Database is what the upgrade reads of the database and the backup it takes.
type Database interface {
	// SchemaVersion is the highest migration recorded.
	SchemaVersion(ctx context.Context) (int64, error)
	// Instances counts every process instance, live and archived, so a
	// restart that lost one is noticed.
	Instances(ctx context.Context) (int64, error)
	// Backup writes a restorable dump to path and returns its size.
	Backup(ctx context.Context, path string) (int64, error)
}
