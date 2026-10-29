package testutils

import (
	"testing"

	"github.com/gsoultan/metis/server/repositories/db"
)

// PostgresDSNEnv names the environment variable holding a DSN for a live
// PostgreSQL instance. Tests that need one skip without it.
const PostgresDSNEnv = "METIS_TEST_POSTGRES_DSN"

// SetupPostgresDB is SetupTestDB with the pool capped at maxConns.
//
// More than one connection is what makes genuine concurrency possible, which
// is the reason the concurrency suites ask for a size at all.
func SetupPostgresDB(t *testing.T, maxConns int) *db.Conn {
	t.Helper()
	return setupTestSchema(t, maxConns)
}
