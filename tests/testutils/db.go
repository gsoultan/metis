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
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/jackc/pgx/v5"
)

// testEncryptionPassphrase is the at-rest key used by every test database.
//
// crypto has no default key by design — process/task variables cannot be
// written until one is configured — so tests must install one explicitly, just
// as the application does at startup.
const testEncryptionPassphrase = "test-only-encryption-passphrase"

// SetupTestDB gives one test its own schema on the configured PostgreSQL, and
// the storm connection onto it.
//
// It used to be an in-memory SQLite file, which was 175 times faster to set up
// and tested a database the product no longer runs on. A suite that passes on
// one engine says nothing about the SQL another emits.
//
// Each test gets its own schema, dropped afterwards, so parallel tests cannot
// see one another's rows. The schema is the one every installation is created
// with, through the runner the application boots with.
func SetupTestDB(t *testing.T) *db.Conn {
	t.Helper()
	return setupTestSchema(t, 0)
}

// SetupTestConn is SetupTestDB under the name the storm-only suites took while
// the two layers lived side by side.
func SetupTestConn(t *testing.T) *db.Conn {
	t.Helper()
	return setupTestSchema(t, 0)
}

// schemaCounter keeps generated schema names short and unique. A UUID would do,
// but PostgreSQL truncates identifiers at 63 bytes and a long prefix leaves
// little room for the index names inside.
var schemaCounter struct {
	sync.Mutex
	n int
}

// setupTestSchema creates the schema and opens a pool onto it. maxConns caps
// the pool when it is more than zero.
func setupTestSchema(t *testing.T, maxConns int) *db.Conn {
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

	ctx := context.Background()
	createSchema(t, dsn, namespace)

	url := stormDSN(dsn, namespace)
	if maxConns > 0 {
		url += fmt.Sprintf("&pool_max_conns=%d", maxConns)
	}
	conn, err := db.Open(ctx, url)
	if err != nil {
		t.Fatalf("open the storm connection: %v", err)
	}
	t.Cleanup(conn.Close)
	if _, err := migrations.Run(ctx, conn.Main(), nil); err != nil {
		t.Fatalf("create the schema: %v", err)
	}
	return conn
}

// createSchema creates namespace and drops it, with everything in it, when the
// test ends.
func createSchema(t *testing.T, dsn, namespace string) {
	t.Helper()
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, serverURL(dsn))
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+namespace); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create schema %s: %v", namespace, err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+namespace+" CASCADE")
		_ = admin.Close(context.Background())
	})
}

// serverURL is the configured database with no schema chosen.
func serverURL(dsn string) string {
	return strings.TrimSuffix(stormDSN(dsn, ""), "&search_path=")
}

// stormDSN turns the key/value DSN the suite is configured with into the URL
// form pgx wants, carrying the schema across.
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
