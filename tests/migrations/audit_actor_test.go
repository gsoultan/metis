package migrations_test

import (
	"strings"
	"testing"

	stormdb "github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm"
)

// An upgrading installation gets audit_logs.actor and the audit log's two
// indexes from migration 46; a fresh one gets them from the storm model. The
// log reads both, so the two must describe the same column and indexes, and
// an entry written before the migration keeps no actor.
func TestMigration46GivesAuditEntriesTheActorTheModelDescribes(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()
	schema := migrations.Schema(models.MigrationModels())
	migrate := func() {
		t.Helper()
		if _, err := migrations.Run(ctx, db, schema); err != nil {
			t.Fatalf("run the migrations: %v", err)
		}
	}
	migrate()

	// The database as an upgrading installation has it: an entry already
	// written, no actor column, and migration 46 not yet run.
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS ix_audit_logs_operations`,
		`DROP INDEX IF EXISTS ix_audit_logs_operations_by_actor`,
		`ALTER TABLE audit_logs DROP COLUMN IF EXISTS actor`,
		`INSERT INTO audit_logs (id, created_at, updated_at, project_id, instance_id, type, message, data)
		 VALUES (gen_random_uuid(), now(), now(), gen_random_uuid(), gen_random_uuid(), 'node_skipped', 'before the upgrade', '{}')`,
	} {
		if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM schema_migrations WHERE version = ?`,
		migrations.AuditActorMigration).Error; err != nil {
		t.Fatalf("forget that migration 46 ran: %v", err)
	}
	migrate()
	migrate() // a second run finds nothing to do

	want, err := storm.Build(model.All()...)
	if err != nil {
		t.Fatalf("build the model: %v", err)
	}
	drift, err := stormdb.ReportDrift(ctx, testutils.StormConn(db).Main(), want)
	if err != nil {
		t.Fatalf("report drift: %v", err)
	}
	// Only what 46 made: a test schema builds the rest of audit_logs through
	// GORM, which spells its other columns and indexes its own way (see
	// tests/drift).
	for _, line := range drift {
		if strings.Contains(line, `"actor"`) || strings.Contains(line, "ix_audit_logs_operations") {
			t.Errorf("the table migration 46 leaves differs from the model: %s", line)
		}
	}
	if !ledgerHasColumn(t, db, "audit_logs", "actor") {
		t.Fatal("migration 46 left out audit_logs.actor")
	}
	for _, index := range []string{"ix_audit_logs_operations", "ix_audit_logs_operations_by_actor"} {
		var definition string
		if err := db.WithContext(ctx).Raw(`SELECT indexdef FROM pg_indexes
			WHERE schemaname = current_schema() AND indexname = ?`, index).Scan(&definition).Error; err != nil {
			t.Fatalf("read %s: %v", index, err)
		}
		if !strings.Contains(definition, "WHERE (actor IS NOT NULL)") {
			t.Errorf("%s is %q, want an index over only the entries that name somebody", index, definition)
		}
	}
	var named int64
	if err := db.WithContext(ctx).Raw(`SELECT count(*) FROM audit_logs WHERE actor IS NOT NULL`).Scan(&named).Error; err != nil {
		t.Fatalf("count the entries with an actor: %v", err)
	}
	if named != 0 {
		t.Errorf("%d entries written before migration 46 were given an actor; nothing could have said who", named)
	}
}
