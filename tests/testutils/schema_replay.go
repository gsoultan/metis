package testutils

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/storm/migrate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// schemaReplay is what the migration runner did to an empty schema, learned
// once per test process and replayed into every later test's schema.
//
// The runner, through storm's migrate.Auto, builds the whole model in a
// scratch schema to normalise it, reads that back, reads the target, diffs
// and only then creates the tables: about a second a test, the same work
// every time. tests/bpmn has more than 400 tests and ran out of its 20
// minutes on CI doing little else.
//
// Replaying is not a second way to create a schema. The plan is the one
// storm computes for an empty schema, which is what the runner applies; the
// first test of a process still goes through the runner; the first replay is
// checked against the model, and fails the test if storm would change
// anything; and every test still calls the runner afterwards, which finds the
// schema recorded and has nothing left to do. The runner's own behaviour on an
// empty database is pinned in tests/migrations, through EmptySchema.
var schemaReplay struct {
	sync.Mutex
	// ddl is every statement of the plan, in order; empty until learned.
	ddl string
	// records are the rows the runner wrote to schema_migrations.
	records []migrationRecord
	// checked is set once a replay has been compared with the model.
	checked bool
}

// migrationRecord is one row of schema_migrations, as the runner wrote it.
type migrationRecord struct {
	version    int64
	name       string
	durationMs int64
}

// buildSchema brings the empty schema pool works in to the model: through the
// runner for the first test of a process, by replay for every one after it.
func buildSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	schemaReplay.Lock()
	learned := schemaReplay.ddl != ""
	if !learned {
		defer schemaReplay.Unlock()
		learnSchema(t, ctx, pool)
		return
	}
	ddl, records, check := schemaReplay.ddl, schemaReplay.records, !schemaReplay.checked
	schemaReplay.checked = true
	schemaReplay.Unlock()

	replaySchema(t, ctx, pool, ddl, records)
	if check {
		requireModelShape(t, ctx, pool)
	}
	if _, err := migrations.Run(ctx, pool, nil); err != nil {
		t.Fatalf("run the migrations on the replayed schema: %v", err)
	}
}

// learnSchema plans the empty schema, then has the runner create it, and keeps
// the plan and what the runner recorded. The caller holds schemaReplay.
func learnSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	ddl, err := planEmptySchema(ctx, pool)
	if err != nil {
		t.Fatalf("plan the schema: %v", err)
	}
	if _, err := migrations.Run(ctx, pool, nil); err != nil {
		t.Fatalf("create the schema: %v", err)
	}
	records, err := readMigrationRecords(ctx, pool)
	if err != nil {
		t.Fatalf("read what the runner recorded: %v", err)
	}
	// A plan with a step that cannot run in a transaction is not one a
	// replay can make all-or-nothing, so every test keeps the runner.
	if ddl == "" {
		return
	}
	schemaReplay.ddl, schemaReplay.records = ddl, records
}

// planEmptySchema is the plan storm makes for the model against the schema
// pool works in, which is still empty: the plan migrate.Auto applies, with
// the options the runner gives it. Empty when the plan cannot be replayed.
func planEmptySchema(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	plan, err := planFor(ctx, pool)
	if err != nil {
		return "", err
	}
	statements := make([]string, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		if change.NoTransaction {
			return "", nil
		}
		statements = append(statements, change.SQL)
	}
	return strings.Join(statements, ";\n"), nil
}

// planFor diffs the schema pool works in against the model.
func planFor(ctx context.Context, pool *pgxpool.Pool) (migrate.Plan, error) {
	want, err := migrations.Target()
	if err != nil {
		return migrate.Plan{}, err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return migrate.Plan{}, err
	}
	defer conn.Release()
	return migrate.ForWith(ctx, conn.Conn(), db.Namespace(pool), want, migrate.Options{DeclaredOnly: true})
}

// replaySchema applies the plan and the runner's records in one transaction.
func replaySchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ddl string, records []migrationRecord) {
	t.Helper()
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('search_path', $1, true)", db.Namespace(pool)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("apply the plan: %w", err)
		}
		return insertMigrationRecords(ctx, tx, records)
	})
	if err != nil {
		t.Fatalf("replay the schema: %v", err)
	}
}

// requireModelShape fails the test unless storm finds the schema already
// matching the model, which is what the runner leaves behind.
func requireModelShape(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	plan, err := planFor(ctx, pool)
	if err != nil {
		t.Fatalf("check the replayed schema: %v", err)
	}
	if !plan.Empty() {
		t.Fatalf("the replayed schema is not the one the runner creates; storm would still apply %d step(s), the first: %s",
			len(plan.Changes), plan.Changes[0].SQL)
	}
}

func readMigrationRecords(ctx context.Context, pool *pgxpool.Pool) ([]migrationRecord, error) {
	rows, err := pool.Query(ctx, `SELECT "version", "name", "duration_ms" FROM "schema_migrations" ORDER BY "version"`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (migrationRecord, error) {
		var record migrationRecord
		err := row.Scan(&record.version, &record.name, &record.durationMs)
		return record, err
	})
}

func insertMigrationRecords(ctx context.Context, tx pgx.Tx, records []migrationRecord) error {
	for _, record := range records {
		if _, err := tx.Exec(ctx,
			`INSERT INTO "schema_migrations" ("version", "name", "applied_at", "duration_ms") VALUES ($1, $2, now(), $3)`,
			record.version, record.name, record.durationMs); err != nil {
			return fmt.Errorf("record migration %d: %w", record.version, err)
		}
	}
	return nil
}
