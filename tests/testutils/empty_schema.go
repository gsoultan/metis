package testutils

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EmptySchema gives one test a schema of its own with nothing in it, and a
// pool whose connections work in it. Dropped when the test ends.
//
// For the tests about what happens to a database before anything has created
// its tables: the migration runner, above all.
func EmptySchema(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := envvar.Get(PostgresDSNEnv)
	if dsn == "" {
		t.Skipf("set %s to run this against a live PostgreSQL instance", PostgresDSNEnv)
	}
	namespace := "e_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	url := stormDSN(dsn, "")
	url = strings.TrimSuffix(url, "&search_path=")

	ctx := context.Background()
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+namespace); err != nil {
		t.Fatalf("create schema %s: %v", namespace, err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(ctx, "DROP SCHEMA IF EXISTS "+namespace+" CASCADE")
		_ = admin.Close(ctx)
	})

	pool, err := db.NewPool(ctx, fmt.Sprintf("%s&search_path=%s", url, namespace))
	if err != nil {
		t.Fatalf("open the schema's pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
