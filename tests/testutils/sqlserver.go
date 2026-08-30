package testutils

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/server/repositories/gorms"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

// SQLServerDSNEnv names the environment variable holding a DSN for a live SQL
// Server instance.
//
// SQL Server is the backend nobody could check. It is offered in the config and
// the setup wizard alongside the other three, and there is no arm64 image — the
// official one segfaults under emulation on Apple silicon — so on a developer
// machine the only options were to reason about it or to leave it alone.
// Reasoning about it is how it came to declare `uuid` columns for four years:
// PostgreSQL and SQLite accept that word, SQL Server does not, and AutoMigrate
// fails on the first table.
//
// So this runs where the machine is amd64, which is every CI runner.
const SQLServerDSNEnv = "GOBPM_TEST_SQLSERVER_DSN"

// SetupSQLServerDB opens the SQL Server instance named by
// GOBPM_TEST_SQLSERVER_DSN and migrates a schema into it, skipping the test when
// no DSN is configured.
func SetupSQLServerDB(t *testing.T, maxConns int) *gorm.DB {
	t.Helper()

	dsn := os.Getenv(SQLServerDSNEnv)
	if dsn == "" {
		t.Skipf("set %s to run this against a live SQL Server instance", SQLServerDSNEnv)
	}

	if err := crypto.Configure(testEncryptionPassphrase); err != nil {
		t.Fatalf("failed to configure test encryption key: %v", err)
	}

	db, err := gorm.Open(sqlserver.Open(dsn), gorms.Config())
	if err != nil {
		t.Fatalf("failed to open sqlserver: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(maxConns)
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := pingWithin(db, 30*time.Second); err != nil {
		t.Fatalf("%s is set but SQL Server is not reachable at that DSN: %v", SQLServerDSNEnv, err)
	}

	// A database per test, as the MySQL helper does, so two packages running
	// together cannot drop each other's tables.
	testDB := "gobpm_test_" + sanitiseSchemaName(t.Name())
	if err := db.Exec("IF DB_ID('" + testDB + "') IS NOT NULL BEGIN ALTER DATABASE " + testDB +
		" SET SINGLE_USER WITH ROLLBACK IMMEDIATE; DROP DATABASE " + testDB + "; END").Error; err != nil {
		t.Fatalf("failed to drop test database: %v", err)
	}
	if err := db.Exec("CREATE DATABASE " + testDB).Error; err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("ALTER DATABASE " + testDB + " SET SINGLE_USER WITH ROLLBACK IMMEDIATE").Error
		_ = db.Exec("DROP DATABASE IF EXISTS " + testDB).Error
	})

	scoped, err := gorm.Open(sqlserver.Open(sqlServerDSNForDatabase(dsn, testDB)), gorms.Config())
	if err != nil {
		t.Fatalf("failed to open the test database: %v", err)
	}
	scopedDB, err := scoped.DB()
	if err != nil {
		t.Fatalf("failed to get scoped sql.DB: %v", err)
	}
	scopedDB.SetMaxOpenConns(maxConns)
	t.Cleanup(func() { _ = scopedDB.Close() })

	if err := scoped.AutoMigrate(migrationModels()...); err != nil {
		t.Fatalf("failed to migrate sqlserver schema: %v", err)
	}
	ensureVersionIndexes(t, scoped)
	return scoped
}

// sqlServerDSNForDatabase swaps the database in a DSN of either supported form:
// sqlserver://user:pass@host:port?database=name, or the key=value form.
func sqlServerDSNForDatabase(dsn, database string) string {
	if i := strings.Index(dsn, "database="); i >= 0 {
		end := strings.IndexByte(dsn[i:], '&')
		if end < 0 {
			return dsn[:i] + "database=" + database
		}
		return dsn[:i] + "database=" + database + dsn[i+end:]
	}
	if strings.Contains(dsn, "?") {
		return dsn + "&database=" + database
	}
	return dsn + "?database=" + database
}
