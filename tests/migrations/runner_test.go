// Package migrations_test proves the schema migration runner against every SQL
// engine the product supports.
//
// This is the upgrade path for every existing deployment, so the cases that
// matter are the ones about a database that already has data: a migration must
// run once and never again, a failure must not be recorded as success, and two
// replicas starting together must not both apply the same change.
package migrations_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/gsoultan/gobpm/server/repositories/migrations"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"github.com/gsoultan/gobpm/tests/testutils"
	"gorm.io/gorm"
)

const testMaxConns = 4

// forEachDialect runs body against every SQL engine the product supports.
func forEachDialect(t *testing.T, body func(t *testing.T, db *gorm.DB)) {
	t.Helper()

	engines := []struct {
		name string
		open func(*testing.T) *gorm.DB
	}{
		{"sqlite", func(t *testing.T) *gorm.DB { return testutils.SetupTestDB(t) }},
		{"postgres", func(t *testing.T) *gorm.DB { return testutils.SetupPostgresDB(t, testMaxConns) }},
		{"mysql", func(t *testing.T) *gorm.DB { return testutils.SetupMySQLDB(t, testMaxConns) }},
	}

	for _, engine := range engines {
		t.Run(engine.name, func(t *testing.T) {
			db := engine.open(t)

			// The bookkeeping tables are not part of migrationModels(), so the
			// MySQL helper — which isolates by dropping and rebuilding that list
			// rather than by using a fresh schema — leaves them behind between
			// tests. A run would then find its versions already applied and do
			// nothing, which looks exactly like a broken runner.
			for _, table := range []string{"schema_migrations", "schema_migration_locks"} {
				if db.Migrator().HasTable(table) {
					if err := db.Migrator().DropTable(table); err != nil {
						t.Fatalf("reset %s: %v", table, err)
					}
				}
			}

			body(t, db)
		})
	}
}

// counter is a migration that records how many times it actually ran.
type counter struct {
	mu   sync.Mutex
	runs int
}

func (c *counter) inc() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.runs++
}

func (c *counter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runs
}

// TestRunAppliesEachMigrationExactlyOnce is the whole point: the two data
// repairs this replaces used to run on every boot, one of them loading every
// process instance ever created into memory to find nothing to do.
func TestRunAppliesEachMigrationExactlyOnce(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		var first, second counter
		list := []migrations.Migration{
			{Version: 1, Name: "first", Run: func(context.Context, *gorm.DB) error { first.inc(); return nil }},
			{Version: 2, Name: "second", Transactional: true, Run: func(context.Context, *gorm.DB) error { second.inc(); return nil }},
		}

		result, err := migrations.Run(t.Context(), db, list)
		if err != nil {
			t.Fatalf("first run: %v", err)
		}
		if len(result.Applied) != 2 {
			t.Fatalf("first run applied %v, want both", result.Applied)
		}

		// A restart must not re-run anything.
		result, err = migrations.Run(t.Context(), db, list)
		if err != nil {
			t.Fatalf("second run: %v", err)
		}
		if len(result.Applied) != 0 {
			t.Errorf("second run applied %v, want nothing", result.Applied)
		}
		if result.Skipped != 2 {
			t.Errorf("second run skipped %d, want 2", result.Skipped)
		}

		if first.count() != 1 || second.count() != 1 {
			t.Fatalf("ran first %d times and second %d, want once each", first.count(), second.count())
		}
	})
}

// TestRunAppliesOnlyNewMigrations covers the upgrade: a database at version 1
// picks up 2 without redoing 1.
func TestRunAppliesOnlyNewMigrations(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		var first, second counter

		v1 := migrations.Migration{Version: 1, Name: "first",
			Run: func(context.Context, *gorm.DB) error { first.inc(); return nil }}
		if _, err := migrations.Run(t.Context(), db, []migrations.Migration{v1}); err != nil {
			t.Fatalf("initial: %v", err)
		}

		v2 := migrations.Migration{Version: 2, Name: "second",
			Run: func(context.Context, *gorm.DB) error { second.inc(); return nil }}
		result, err := migrations.Run(t.Context(), db, []migrations.Migration{v1, v2})
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}

		if len(result.Applied) != 1 || result.Applied[0] != 2 {
			t.Errorf("applied %v, want just version 2", result.Applied)
		}
		if first.count() != 1 {
			t.Errorf("version 1 ran %d times across the upgrade, want 1", first.count())
		}
	})
}

// TestRunStopsAtFirstFailure keeps a broken migration from being recorded, and
// keeps later ones from running against a schema the failed one should have
// produced.
func TestRunStopsAtFirstFailure(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		wantErr := errors.New("migration blew up")
		var later counter

		list := []migrations.Migration{
			{Version: 1, Name: "ok", Run: func(context.Context, *gorm.DB) error { return nil }},
			{Version: 2, Name: "broken", Run: func(context.Context, *gorm.DB) error { return wantErr }},
			{Version: 3, Name: "later", Run: func(context.Context, *gorm.DB) error { later.inc(); return nil }},
		}

		_, err := migrations.Run(t.Context(), db, list)
		if !errors.Is(err, wantErr) {
			t.Fatalf("err = %v, want it to wrap %v", err, wantErr)
		}
		if later.count() != 0 {
			t.Error("a migration after the failure ran anyway")
		}

		// The failed migration must not be recorded, or a retry would skip it.
		var applied []int
		if err := db.Model(&migrations.SchemaMigration{}).Order("version").Pluck("version", &applied).Error; err != nil {
			t.Fatalf("read applied: %v", err)
		}
		if len(applied) != 1 || applied[0] != 1 {
			t.Fatalf("recorded %v, want only version 1 — a failure was recorded as success", applied)
		}
	})
}

// TestRunIsSafeAcrossConcurrentReplicas is the deployment case: replicas start
// together, and without the lock several would apply the same migration at once.
func TestRunIsSafeAcrossConcurrentReplicas(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		var applied counter
		list := []migrations.Migration{
			{Version: 1, Name: "contended", Run: func(context.Context, *gorm.DB) error { applied.inc(); return nil }},
		}

		const replicas = 4
		errs := make(chan error, replicas)
		var wg sync.WaitGroup
		for range replicas {
			wg.Go(func() {
				_, err := migrations.Run(t.Context(), db, list)
				errs <- err
			})
		}
		wg.Wait()
		close(errs)

		for err := range errs {
			if err != nil {
				t.Errorf("a replica failed to migrate: %v", err)
			}
		}
		if got := applied.count(); got != 1 {
			t.Fatalf("the migration ran %d times across %d replicas, want exactly 1", got, replicas)
		}
	})
}

// TestRunRejectsDuplicateVersions catches the merge accident where two branches
// each add the same version. Undetected, whichever ran first would permanently
// mask the other.
func TestRunRejectsDuplicateVersions(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		_, err := migrations.Run(t.Context(), db, []migrations.Migration{
			{Version: 7, Name: "from one branch", Run: func(context.Context, *gorm.DB) error { return nil }},
			{Version: 7, Name: "from another", Run: func(context.Context, *gorm.DB) error { return nil }},
		})
		if err == nil {
			t.Fatal("duplicate versions were accepted")
		}
	})
}

// TestRunAppliesInVersionOrder pins ordering regardless of how the list is
// assembled, since the list is composed from more than one place.
func TestRunAppliesInVersionOrder(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		var order []int
		var mu sync.Mutex
		record := func(v int) func(context.Context, *gorm.DB) error {
			return func(context.Context, *gorm.DB) error {
				mu.Lock()
				defer mu.Unlock()
				order = append(order, v)
				return nil
			}
		}

		// Deliberately out of order.
		_, err := migrations.Run(t.Context(), db, []migrations.Migration{
			{Version: 3, Name: "third", Run: record(3)},
			{Version: 1, Name: "first", Run: record(1)},
			{Version: 2, Name: "second", Run: record(2)},
		})
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if len(order) != 3 {
			t.Fatalf("applied %v, want all three", order)
		}
		for i, want := range []int{1, 2, 3} {
			if order[i] != want {
				t.Fatalf("applied in order %v, want ascending", order)
			}
		}
	})
}

// TestBaselineIsIdempotentOnAnExistingDatabase is the migration-adoption case:
// every existing installation already has this schema from AutoMigrate, so the
// baseline must be a no-op there rather than an error.
func TestBaselineIsIdempotentOnAnExistingDatabase(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		// SetupTestDB already AutoMigrated the models, which is exactly the
		// state a pre-migrations installation is in.
		schema := migrations.Schema(models.MigrationModels())

		if _, err := migrations.Run(t.Context(), db, schema); err != nil {
			t.Fatalf("baseline against an existing schema: %v", err)
		}

		// And the data that was already there survives it.
		var drift []string
		drift, err := migrations.DriftReport(db, models.MigrationModels())
		if err != nil {
			t.Fatalf("drift report: %v", err)
		}
		if len(drift) != 0 {
			t.Fatalf("baseline left the schema short of the models: %v", drift)
		}
	})
}

// TestDriftReportNoticesAMissingColumn proves the guard rail that replaces
// AutoMigrate's habit of silently adding columns.
func TestDriftReportNoticesAMissingColumn(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		if err := db.Migrator().DropColumn(&models.TaskModel{}, "description"); err != nil {
			t.Fatalf("drop column: %v", err)
		}

		drift, err := migrations.DriftReport(db, models.MigrationModels())
		if err != nil {
			t.Fatalf("drift report: %v", err)
		}

		var found bool
		for _, item := range drift {
			if item == "tasks.description is declared by a model but missing from the database" {
				found = true
			}
		}
		if !found {
			t.Fatalf("drift report did not notice the dropped column, got %v", drift)
		}
	})
}
