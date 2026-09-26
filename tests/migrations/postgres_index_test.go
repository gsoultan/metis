package migrations_test

import (
	"testing"

	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// The migration list, run against a real PostgreSQL.
//
// The completeness tests beside this one run it over in-memory SQLite, on
// purpose: they owe nothing to a test harness, and that is what makes them able
// to catch a table no migration creates. The cost is that they cannot see any
// statement PostgreSQL alone understands — and migration 20 builds its indexes
// CONCURRENTLY, which is PostgreSQL-only and takes a different branch there.
//
// So a PostgreSQL-only migration bug would ship green. This is the other half.
func TestMigrationsProduceValidIndexesOnPostgres(t *testing.T) {
	db := testutils.SetupTestDB(t)

	if _, err := migrations.Run(t.Context(), db, migrations.Schema(models.MigrationModels())); err != nil {
		t.Fatalf("run the migrations: %v", err)
	}

	// Validity, not existence. A CONCURRENTLY build that fails part-way leaves
	// the catalog row behind marked invalid: every "does it exist?" check says
	// yes for ever while the planner refuses to use it. An index that is present
	// and unusable is worse than one that is absent, because nothing reports it.
	for _, name := range []string{
		"ix_process_instances_project_created",
		"ix_process_instances_project_status",
		"ix_notifications_user_unread",
		"ix_notifications_user_newest",
	} {
		var valid bool
		err := db.Raw(`
			SELECT i.indisvalid
			  FROM pg_class c
			  JOIN pg_index i ON i.indexrelid = c.oid
			 WHERE c.relname = ?
			   AND c.relnamespace = current_schema()::regnamespace`, name).Scan(&valid).Error
		if err != nil {
			t.Fatalf("check %s: %v", name, err)
		}
		if !valid {
			t.Errorf("%s is missing or INVALID; the reads it is for fall back to scanning the table", name)
		}
	}

	// Applying the list twice must be a no-op rather than an error — every
	// replica in a deployment runs it, and an upgrade retried after a failure
	// runs it again.
	if _, err := migrations.Run(t.Context(), db, migrations.Schema(models.MigrationModels())); err != nil {
		t.Fatalf("run the migrations a second time: %v", err)
	}
}
