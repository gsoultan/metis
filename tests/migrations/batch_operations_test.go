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

// The batch operations reach a database two ways: an upgrading installation
// runs migration 45, whose DDL is frozen text, and a fresh one or a test schema
// gets the tables from the storm model. The worker reads both, so the two must
// describe the same tables.
func TestMigration45CreatesTheBatchOperationsAsTheModelDescribesThem(t *testing.T) {
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

	// The database as an upgrading installation has it: no batch operations,
	// and migration 45 not yet run.
	for _, table := range []string{"batch_operation_items", "batch_operations"} {
		if err := db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS ` + table).Error; err != nil {
			t.Fatalf("remove %s: %v", table, err)
		}
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM schema_migrations WHERE version = ?`,
		migrations.BatchOperationsMigration).Error; err != nil {
		t.Fatalf("forget that migration 45 ran: %v", err)
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
		if strings.Contains(line, "batch_operation") {
			t.Errorf("the tables migration 45 leaves differ from the model: %s", line)
		}
	}
	for _, column := range []string{"project_id", "organization_id", "kind", "status", "total", "lease_expires"} {
		if !ledgerHasColumn(t, db, "batch_operations", column) {
			t.Errorf("migration 45 left out batch_operations.%s", column)
		}
	}
	for _, column := range []string{"batch_operation_id", "instance_id", "status", "detail", "settled_at"} {
		if !ledgerHasColumn(t, db, "batch_operation_items", column) {
			t.Errorf("migration 45 left out batch_operation_items.%s", column)
		}
	}
}
