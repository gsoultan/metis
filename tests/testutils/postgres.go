package testutils

import (
	"testing"

	"gorm.io/gorm"
)

// PostgresDSNEnv names the environment variable holding a DSN for a live
// PostgreSQL instance.
//
// The rest of the suite runs on in-memory SQLite with a single connection, which
// serialises every transaction. That is fine for behaviour but it cannot show
// anything about concurrency, and it does not exercise the SQL a real deployment
// runs. Tests that need either of those ask for this DSN and skip without it, so
// the default gate stays hermetic.
const PostgresDSNEnv = "METIS_TEST_POSTGRES_DSN"

// SetupPostgresDB opens the PostgreSQL instance named by METIS_TEST_POSTGRES_DSN
// and migrates a schema into it, skipping the test when no DSN is configured.
//
// maxConns controls the connection pool. More than one connection is what makes
// genuine concurrency possible, which is the whole point of reaching for a real
// database here.
func SetupPostgresDB(t *testing.T, maxConns int) *gorm.DB {
	t.Helper()

	// The same schema-per-test path SetupTestDB uses. It was a separate helper
	// while SetupTestDB meant SQLite and "run this against a real database" was
	// a thing a suite opted into; both now mean the same thing, and two helpers
	// meant only one of them registered the storm connection that the ported
	// repositories need.
	//
	// maxConns is honoured because concurrency is the reason these suites
	// reached for a real database in the first place.
	gormDB, _ := setupTestSchema(t)
	if sqlDB, err := gormDB.DB(); err == nil {
		sqlDB.SetMaxOpenConns(maxConns)
	}
	return gormDB
}
