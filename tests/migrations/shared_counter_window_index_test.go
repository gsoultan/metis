package migrations_test

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// pruneBatch is the row-picking half of the statement the shared-counter
// sweep sends, as it sends it.
const pruneBatch = `SELECT ctid FROM shared_counters WHERE window_start < $1 LIMIT $2`

// The rate-limit sweep finds the windows that have closed from an index, not
// by scanning the table once for every five thousand rows it removes.
//
// window_start is the last column of the primary key, so the key's index
// cannot answer a range on it alone: before migration 34 the cheapest plan
// for each batch was a pass over every counter in the table.
func TestTheRateLimitSweepFindsClosedWindowsFromAnIndex(t *testing.T) {
	db := setupMigrated(t)
	assertValidIndex(t, db, "ix_shared_counters_window_start", "shared_counters USING btree (window_start)")

	// Mostly open windows, as the table is between sweeps, and a few closed.
	if err := db.WithContext(t.Context()).Exec(`
		INSERT INTO shared_counters (scope, counter_key, replica, window_start, count, updated_at)
		SELECT 'http-rate', 'client-' || n, 'replica-a',
		       date_trunc('minute', now()) - CASE WHEN n <= 50 THEN interval '1 hour' ELSE interval '0' END,
		       1, now()
		  FROM generate_series(1, 5000) AS n`).Error; err != nil {
		t.Fatalf("seed the counters: %v", err)
	}
	if err := db.WithContext(t.Context()).Exec(`ANALYZE shared_counters`).Error; err != nil {
		t.Fatalf("analyze: %v", err)
	}

	plans := explainGeneric(t, db,
		map[string]string{"prune": pruneBatch},
		map[string]string{"prune": `now() - interval '5 minutes', 5000`})
	if !strings.Contains(plans["prune"], "ix_shared_counters_window_start") {
		t.Fatalf("a sweep batch is not answered from ix_shared_counters_window_start:\n%s", plans["prune"])
	}
}

// Migration 34 is only reachable on an installation that already has the
// table, with counters in it. This takes the index away and forgets the
// migration ran, the way an upgrading installation arrives, and runs it again.
func TestMigration34IndexesASharedCounterTableThatAlreadyHasRows(t *testing.T) {
	db := setupMigrated(t)
	if err := db.WithContext(t.Context()).Exec(`DROP INDEX IF EXISTS ix_shared_counters_window_start`).Error; err != nil {
		t.Fatalf("drop the index: %v", err)
	}
	if err := db.WithContext(t.Context()).Exec(
		`DELETE FROM schema_migrations WHERE version = ?`, migrations.SharedCounterWindowIndexMigration).Error; err != nil {
		t.Fatalf("forget the migration ran: %v", err)
	}
	if err := db.WithContext(t.Context()).Exec(`
		INSERT INTO shared_counters (scope, counter_key, replica, window_start, count, updated_at)
		VALUES ('http-rate', 'client', 'replica-a', date_trunc('minute', now()), 3, now())`).Error; err != nil {
		t.Fatalf("seed a counter: %v", err)
	}

	migrateAll(t, db)
	migrateAll(t, db) // and a second run finds nothing left to do

	assertValidIndex(t, db, "ix_shared_counters_window_start", "shared_counters USING btree (window_start)")
}

// assertValidIndex checks an index is there, usable, and over what it is for.
func assertValidIndex(t *testing.T, db *gorm.DB, name, over string) {
	t.Helper()
	var index struct {
		Definition string
		Valid      bool
	}
	if err := db.WithContext(t.Context()).Raw(`
		SELECT pg_get_indexdef(i.indexrelid) AS definition, i.indisvalid AS valid
		  FROM pg_class c
		  JOIN pg_index i ON i.indexrelid = c.oid
		 WHERE c.relname = ?
		   AND c.relnamespace = current_schema()::regnamespace`, name).Scan(&index).Error; err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if !index.Valid {
		t.Fatalf("%s is missing or INVALID: %q", name, index.Definition)
	}
	if !strings.Contains(index.Definition, over) {
		t.Fatalf("%s is not over %s: %s", name, over, index.Definition)
	}
}

// setupMigrated is a schema with every migration applied, as an installation
// that has run this release has it.
func setupMigrated(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutils.SetupTestDB(t)
	migrateAll(t, db)
	return db
}
