package environment_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/gorms"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/store/organization"
	"github.com/gsoultan/storm/runtime/pgxdrv"
	pgschema "github.com/gsoultan/storm/schema/pg"
)

// A saved environment row is enough to open and migrate its database.
//
// This is what turns the registry from a record of intent into something the
// server can serve: the row names a driver and a database, and nothing else has
// to be configured for that runtime to exist.
func TestAnEnvironmentRowOpensAndMigratesItsDatabase(t *testing.T) {
	ctx := entities.WithSystemContext(t.Context())
	pool, err := db.NewPool(ctx, config.PostgresURL(environmentDSN(t)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(pool.Close)

	// Opening does not contact the database; asking it something does.
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	result, err := migrations.Run(ctx, pool, migrations.Steps())
	if err != nil {
		t.Fatalf("migrate the environment database: %v", err)
	}
	if !result.Created {
		t.Fatal("a fresh environment database should have been created")
	}

	// The runtime tables are what an environment holds.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("borrow a connection: %v", err)
	}
	live, err := pgschema.Introspect(ctx, conn.Conn(), db.Namespace(pool))
	conn.Release()
	if err != nil {
		t.Fatalf("read the schema: %v", err)
	}
	have := map[string]bool{}
	for _, table := range live.Tables {
		have[table.Name] = true
	}
	for _, table := range []string{"process_definitions", "process_instances", "tasks", "jobs"} {
		if !have[table] {
			t.Errorf("an environment database needs %s", table)
		}
	}

	// And it is genuinely a different database: writing here leaves the main
	// one alone, which is the whole guarantee.
	org := organization.Create()
	org.SetID(uuid.Must(uuid.NewV7()))
	org.SetName("only here")
	if _, err := org.Insert(ctx, pgxdrv.Pool{P: pool}); err != nil {
		t.Fatalf("write to the environment database: %v", err)
	}
}

// The registry hands back the connection for an environment, and says plainly
// when there is none.
func TestTheRegistryTracksConnectionsPerEnvironment(t *testing.T) {
	ctx := t.Context()
	dsn := config.PostgresURL(environmentDSN(t))
	conn, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(conn.Close)

	staging, production := uuid.New(), uuid.New()
	stagingPool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(stagingPool.Close)

	if _, ok := conn.EnvironmentPools()[staging]; ok {
		t.Fatal("nothing is registered yet")
	}

	if replaced := conn.RegisterEnvironment(staging, stagingPool); replaced != nil {
		t.Fatal("registering the first connection replaces nothing")
	}

	got, ok := conn.EnvironmentPools()[staging]
	if !ok || got != stagingPool {
		t.Fatal("the registered connection should come back")
	}

	// An environment nobody registered is a normal answer, not an error: it can
	// be disabled, or its database can have been unreachable at boot.
	if _, ok := conn.EnvironmentPools()[production]; ok {
		t.Fatal("an unregistered environment has no connection")
	}

	// Re-opening an environment hands back the old handle so the caller can
	// close it, rather than closing it out from under requests in flight.
	replacement, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open replacement: %v", err)
	}
	t.Cleanup(replacement.Close)
	if replaced := conn.RegisterEnvironment(staging, replacement); replaced != stagingPool {
		t.Fatal("re-registering should hand back the connection it displaced")
	}

	if _, ok := conn.ForgetEnvironment(staging); !ok {
		t.Fatal("forgetting a registered environment should report what it dropped")
	}
	if _, ok := conn.EnvironmentPools()[staging]; ok {
		t.Fatal("a forgotten environment has no connection")
	}
}

// A database that is not there fails at open, not at the first request.
//
// Opening in GORM does not contact the server, so without an explicit ping a
// misconfigured environment would register as ready and fail later, away from
// whoever was watching the boot.
func TestAnUnreachableDatabaseIsRefusedAtOpen(t *testing.T) {
	db, err := gorms.Open("postgres", "host=127.0.0.1 port=1 user=nobody password=x dbname=nothing sslmode=disable")
	if err != nil {
		return // refused already, which is also correct
	}
	t.Cleanup(func() {
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	if err := gorms.Ping(db); err == nil {
		t.Fatal("a database on a closed port should not answer")
	}
}

// environmentDSN gives each test its own schema on the configured PostgreSQL,
// so two of them cannot see one another's tables.
//
// A file used to do this job: an environment's database was a SQLite file in a
// temp directory, which needed nothing configured. That is gone with SQLite,
// and the cost is that these skip without a DSN — which is why the CI job that
// provides one also fails on a skip.
func environmentDSN(t *testing.T) string {
	t.Helper()
	base := os.Getenv("METIS_TEST_POSTGRES_DSN")
	if base == "" {
		t.Skip("set METIS_TEST_POSTGRES_DSN to run this against a live PostgreSQL instance")
	}

	namespace := "env_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	admin, err := gorms.Open("postgres", base)
	if err != nil {
		t.Fatalf("open the admin connection: %v", err)
	}
	if err := admin.Exec("CREATE SCHEMA " + namespace).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_ = admin.Exec("DROP SCHEMA IF EXISTS " + namespace + " CASCADE").Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return fmt.Sprintf("%s search_path=%s", base, namespace)
}
