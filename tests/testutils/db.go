package testutils

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/gorms"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/storm"
	"gorm.io/gorm"
)

// testEncryptionPassphrase is the at-rest key used by every test database.
//
// crypto has no default key by design — process/task variables cannot be
// written until one is configured — so tests must install one explicitly, just
// as the application does at startup.
const testEncryptionPassphrase = "test-only-encryption-passphrase"

// SetupTestDB gives one test its own schema on the configured PostgreSQL.
//
// It used to be an in-memory SQLite file, which was 175 times faster to set up
// and tested a database the product no longer runs on. That was tolerable while
// SQLite was supported and became a fiction the moment it was not: a suite that
// passes on one engine says nothing about the SQL another emits, which is the
// class of bug the dialect suites existed to catch and never ran often enough
// to catch.
//
// It is also what makes the storm port checkable. Both layers read the same
// schema through the same search_path, so a repository that has moved and one
// that has not are provably looking at the same rows.
//
// Each test gets its own schema, dropped afterwards, so parallel tests cannot
// see one another's rows.
func SetupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gormDB, _ := setupTestSchema(t)
	return gormDB
}

// SetupTestStore gives one test both handles onto the same schema: the GORM one
// for repositories that have not moved, and the storm one for those that have.
func SetupTestStore(t *testing.T) (*gorm.DB, *db.Conn) {
	t.Helper()
	return setupTestSchema(t)
}

// schemaCounter keeps generated schema names short and unique. A UUID would do,
// but PostgreSQL truncates identifiers at 63 bytes and a long prefix leaves
// little room for the index names inside.
var schemaCounter struct {
	sync.Mutex
	n int
}

func setupTestSchema(t *testing.T) (*gorm.DB, *db.Conn) {
	t.Helper()

	dsn := envvar.Get(PostgresDSNEnv)
	if dsn == "" {
		t.Skipf("set %s to run this against a live PostgreSQL instance", PostgresDSNEnv)
	}
	if err := crypto.Configure(testEncryptionPassphrase); err != nil {
		t.Fatalf("failed to configure test encryption key: %v", err)
	}

	schemaCounter.Lock()
	schemaCounter.n++
	namespace := fmt.Sprintf("t%d_%s", schemaCounter.n, strings.ReplaceAll(uuid.NewString()[:6], "-", ""))
	schemaCounter.Unlock()

	admin, err := gorms.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := admin.Exec("CREATE SCHEMA " + namespace).Error; err != nil {
		t.Fatalf("create schema %s: %v", namespace, err)
	}
	t.Cleanup(func() {
		_ = admin.Exec("DROP SCHEMA IF EXISTS " + namespace + " CASCADE").Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	// search_path in the DSN rather than a SET on one session: both layers open
	// their own pool, and a session setting does not survive a pool that hands
	// out a different connection next time.
	scoped := dsn + " search_path=" + namespace
	gormDB, err := gorms.Open("postgres", scoped)
	if err != nil {
		t.Fatalf("open the scoped connection: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := gormDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	if err := gormDB.AutoMigrate(migrationModels()...); err != nil {
		t.Fatalf("failed to migrate db: %v", err)
	}
	ensureVersionIndexes(t, gormDB)
	ensureAuditWriteOrder(t, gormDB)

	conn, err := db.Open(context.Background(), stormDSN(dsn, namespace))
	if err != nil {
		t.Fatalf("open the storm connection: %v", err)
	}
	t.Cleanup(conn.Close)
	if err := createStormTables(t, conn); err != nil {
		t.Fatalf("create the storm tables: %v", err)
	}
	stormConns.Store(gormDB, conn)
	t.Cleanup(func() { stormConns.Delete(gormDB) })
	return gormDB, conn
}

// stormDSN turns the key/value DSN the GORM helpers take into the URL form pgx
// wants, carrying the schema across.
//
// Two formats for one database is not a choice anybody made; it is what the two
// drivers accept. Converting here means a test configures one DSN and both
// layers reach the same schema, which is the property that makes a half-ported
// repository layer testable at all.
func stormDSN(dsn, namespace string) string {
	fields := map[string]string{}
	for _, pair := range strings.Fields(dsn) {
		key, value, ok := strings.Cut(pair, "=")
		if ok {
			fields[key] = value
		}
	}
	url := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s&search_path=%s",
		fields["user"], fields["password"], fields["host"], fields["port"],
		fields["dbname"], sslModeOr(fields["sslmode"]), namespace)
	return url
}

func sslModeOr(mode string) string {
	if mode == "" {
		return "disable"
	}
	return mode
}
func ensureVersionIndexes(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := migrations.EnsureVersionIndexes(db, migrationModels()); err != nil {
		t.Fatalf("failed to create version indexes: %v", err)
	}
}

// ensureAuditWriteOrder gives the audit table the sequence migration 28 puts
// behind audit_logs.seq. AutoMigrate creates the column and not the sequence,
// and a trail read without it would come back in storage order — the defect
// the migration closes.
func ensureAuditWriteOrder(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := migrations.EnsureAuditWriteOrder(context.Background(), db); err != nil {
		t.Fatalf("failed to number audit entries as they are written: %v", err)
	}
}

// createStormTables applies the storm model's DDL for tables the GORM models do
// not describe.
//
// Both layers share the schema, so whichever runs second must create only what
// the first did not — see db.EnsureTables, which is the same function the
// application boots with.
func createStormTables(t *testing.T, conn *db.Conn) error {
	t.Helper()
	want, err := storm.Build(model.All()...)
	if err != nil {
		return err
	}
	if _, err := db.EnsureTables(context.Background(), conn.Main(), want); err != nil {
		return err
	}
	// The same reconciliation the application performs at boot: AutoMigrate has
	// just created the shared tables without the defaults storm writes against.
	_, err = db.EnsureColumnDefaults(context.Background(), conn.Main(), want)
	return err
}

// stormConns remembers the storm connection that shares a schema with a GORM
// one, keyed by the GORM handle a test already holds.
//
// A registry rather than a second return value because fifty-three call sites
// already say `repositories.NewRepository(testutils.StormConn(db))`, and threading a second handle
// through all of them by hand is fifty-three chances to pair a GORM connection
// with somebody else's schema. Looking it up cannot get that wrong: the key is
// the connection the test was given.
var stormConns sync.Map

// StormConn returns the storm connection onto the same schema as db.
//
// Nil for a handle this package did not create, which NewRepository refuses —
// a repository layer half-connected to two different databases is the failure
// this exists to prevent, and it must not be quiet.
func StormConn(gormDB *gorm.DB) *db.Conn {
	if conn, ok := stormConns.Load(gormDB); ok {
		if c, ok := conn.(*db.Conn); ok {
			return c
		}
	}
	return nil
}

// SetupTestConn gives one test its own schema and the storm connection onto it.
//
// The repository facade takes only this now — no repository reads through GORM,
// so handing one out alongside it invited a test to write through a connection
// nothing else was looking at.
func SetupTestConn(t *testing.T) *db.Conn {
	t.Helper()
	_, conn := setupTestSchema(t)
	return conn
}
