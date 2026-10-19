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

// The holds reach a database two ways: an upgrading installation runs
// migration 37, whose DDL is frozen text, and a fresh one or a test schema
// gets the table from the storm model. A hold the sweep cannot read is an
// instance it removes, so the two must describe one table.
func TestMigration37CreatesTheHoldsAsTheModelDescribesThem(t *testing.T) {
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

	// The database as an upgrading installation has it: no holds, and
	// migration 37 not yet run.
	if err := db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS retention_holds`).Error; err != nil {
		t.Fatalf("remove the holds: %v", err)
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM schema_migrations WHERE version = ?`,
		migrations.RetentionHoldsMigration).Error; err != nil {
		t.Fatalf("forget that migration 37 ran: %v", err)
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
	for _, line := range drift {
		if strings.Contains(line, "retention_holds") {
			t.Errorf("the table migration 37 leaves differs from the model: %s", line)
		}
	}
	for _, column := range []string{"instance_id", "project_id", "reason", "placed_by", "placed_by_id"} {
		if !ledgerHasColumn(t, db, "retention_holds", column) {
			t.Errorf("migration 37 left out retention_holds.%s", column)
		}
	}
}
