package impl

import (
	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// The variables a server starts from when there is no config.yaml: the
// database (resolveDialector) and the two secrets (setupEncryption and
// resolveJWTSecret in internal/app).
const (
	envDatabaseURL   = "DATABASE_URL"
	envEncryptionKey = "ENCRYPTION_KEY"
	envJWTSecret     = "JWT_SECRET"
)

// setupTarget is the database setup initializes, and whether setup has to
// record which one it was.
//
// Two shapes. A server whose environment names its database and both secrets
// is configured already, except for the people in it: setup seeds that
// database and writes nothing — which is also the only thing it can do on the
// read-only root the container deployments run on, where the wizard used to
// seed the administrator and then fail writing config.yaml, every time.
// Otherwise setup is the wizard it always was: it seeds the database the
// request names and writes config.yaml, which from then on takes priority
// over DATABASE_URL.
type setupTarget struct {
	driver string
	dsn    string
	// fromEnvironment means the environment named this database and the
	// secrets, so there is no file to write and no key to install.
	fromEnvironment bool
}

func setupTargetFor(req contracts.SetupRequest) setupTarget {
	if configuredByEnvironment() {
		return setupTarget{
			driver:          config.DriverPostgres,
			dsn:             envvar.Get(envDatabaseURL),
			fromEnvironment: true,
		}
	}
	return setupTarget{
		driver: req.DatabaseDriver,
		dsn:    config.BuildConnectionString(req.DatabaseDriver, buildDatabaseFields(req)),
	}
}

// configuredByEnvironment reports whether the environment already says
// everything setup would otherwise write down.
//
// With all three set and no config.yaml, a file written now would only repeat
// them — and a request naming a different database would get a config.yaml
// that quietly outranks DATABASE_URL on the next restart, moving the server
// onto whatever that request pointed at.
func configuredByEnvironment() bool {
	if config.Exists(config.DefaultConfigPath) {
		return false
	}
	for _, name := range []string{envDatabaseURL, envEncryptionKey, envJWTSecret} {
		if envvar.Get(name) == "" {
			return false
		}
	}
	return true
}
