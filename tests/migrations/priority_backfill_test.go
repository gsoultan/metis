package migrations_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// TestPriorityBackfillRunsAgainstTheSchemaItWasWrittenFor.
//
// Migration 21 is only reachable *from* the old schema: a fresh install gets a
// NOT NULL priority column from the model tag and the migration finds nothing
// to do. So on a fresh install it is never really executed, and the one
// installation that needs it — the one upgrading, with a tasks table full of
// NULLs that AutoMigrate added the column to — would be the first to run it.
// docs/upgrading.md says this is how a migration ships broken, having been
// written, reviewed and never executed against the table it was written for.
//
// This puts the table back the way it was, fills it with the rows that panicked
// the inbox, and runs the migration at it.
func TestPriorityBackfillRunsAgainstTheSchemaItWasWrittenFor(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()

	if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
		t.Fatalf("run the migrations: %v", err)
	}

	// Back to the pre-21 shape: nullable, no default.
	for _, stmt := range []string{
		`ALTER TABLE tasks ALTER COLUMN priority DROP NOT NULL`,
		`ALTER TABLE tasks ALTER COLUMN priority DROP DEFAULT`,
		`DELETE FROM schema_migrations WHERE version = 21`,
	} {
		if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
			t.Fatalf("restore the old schema (%s): %v", stmt, err)
		}
	}

	// The rows the engine never writes and everything else does.
	const nullRows = 3
	for range nullRows {
		if err := db.WithContext(ctx).Exec(`
			INSERT INTO tasks (id, created_at, updated_at, project_id, instance_id,
			                   node_id, name, type, status, priority, variables)
			VALUES (?, now(), now(), ?, ?, 'approve', 'Approve', 'userTask', 'unclaimed', NULL, '')`,
			uuid.New(), uuid.New(), uuid.New()).Error; err != nil {
			t.Fatalf("write a task with a NULL priority: %v", err)
		}
	}

	if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
		t.Fatalf("re-run the migrations over the old schema: %v", err)
	}

	var remaining int64
	if err := db.WithContext(ctx).Raw(
		`SELECT count(*) FROM tasks WHERE priority IS NULL`).Scan(&remaining).Error; err != nil {
		t.Fatalf("count the NULLs: %v", err)
	}
	if remaining != 0 {
		t.Errorf("%d tasks still carry a NULL priority; every read of the inbox panics on them", remaining)
	}

	var backfilled int64
	if err := db.WithContext(ctx).Raw(
		`SELECT count(*) FROM tasks WHERE priority = 0`).Scan(&backfilled).Error; err != nil {
		t.Fatalf("count the backfilled rows: %v", err)
	}
	if backfilled != nullRows {
		t.Errorf("backfilled %d rows, want %d", backfilled, nullRows)
	}

	// The constraint, not just the data: without it the next bulk insert puts
	// the NULLs straight back.
	var nullable string
	if err := db.WithContext(ctx).Raw(`
		SELECT is_nullable FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'tasks' AND column_name = 'priority'`).
		Scan(&nullable).Error; err != nil {
		t.Fatalf("read the column definition: %v", err)
	}
	if nullable != "NO" {
		t.Errorf("tasks.priority is still nullable (%q)", nullable)
	}

	// And the check constraint is not left behind holding a lock on every write.
	var leftover int64
	if err := db.WithContext(ctx).Raw(`
		SELECT count(*) FROM pg_constraint WHERE conname = 'ck_tasks_priority_not_null'`).
		Scan(&leftover).Error; err != nil {
		t.Fatalf("look for the scaffolding constraint: %v", err)
	}
	if leftover != 0 {
		t.Errorf("the NOT VALID check used to avoid the table scan was left behind")
	}
}
