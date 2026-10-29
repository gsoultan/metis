package testutils

import (
	"context"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/jackc/pgx/v5"
)

// CreateDatabase creates a database on the test's PostgreSQL server, dropped
// when the test ends.
//
// A database rather than the schema the rest of the suite gets, for what is
// opened by a connection that names a database and nothing narrower: an
// environment's, above all.
func CreateDatabase(t *testing.T, name string) {
	t.Helper()
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, serverURL(configuredDSN(t)))
	if err != nil {
		t.Fatalf("open the maintenance connection: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create %s: %v", name, err)
	}
	t.Cleanup(func() {
		defer func() { _ = admin.Close(ctx) }()
		// FORCE, because a pool that has not finished closing still holds
		// connections to it — and a database left behind on a shared server is
		// somebody else's problem tomorrow.
		if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Logf("could not drop %s: %v", name, err)
		}
	})
}

// OpenDatabase opens a database on the test's PostgreSQL server as it is,
// creating nothing in it. Closed when the test ends.
func OpenDatabase(t *testing.T, name string) *db.Conn {
	t.Helper()
	conn, err := db.Open(context.Background(), DatabaseURL(t, name))
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(conn.Close)
	return conn
}

// DatabaseURL is the connection URL for a database on the test's server.
func DatabaseURL(t *testing.T, name string) string {
	t.Helper()
	fields := strings.Fields(configuredDSN(t))
	for i, pair := range fields {
		if key, _, ok := strings.Cut(pair, "="); ok && key == "dbname" {
			fields[i] = "dbname=" + name
		}
	}
	return serverURL(strings.Join(fields, " "))
}

// configuredDSN is the suite's DSN, or a skip when there is none.
func configuredDSN(t *testing.T) string {
	t.Helper()
	dsn := envvar.Get(PostgresDSNEnv)
	if dsn == "" {
		t.Skipf("set %s to run this against a live PostgreSQL instance", PostgresDSNEnv)
	}
	return dsn
}
