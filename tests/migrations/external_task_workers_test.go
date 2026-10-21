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

// An upgrading installation gets the workers' table from migration 41 and a
// fresh one from the model. A fetch writes to it on every poll, keyed by its
// unique index, so the two descriptions must be of one table.
func TestMigration41CreatesTheExternalTaskWorkersAsTheModelDescribesThem(t *testing.T) {
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

	// The database as an upgrading installation has it: no workers' table,
	// and migration 41 not yet run.
	if err := db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS external_task_workers`).Error; err != nil {
		t.Fatalf("remove the workers' table: %v", err)
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM schema_migrations WHERE version = ?`,
		migrations.ExternalTaskWorkersMigration).Error; err != nil {
		t.Fatalf("forget that migration 41 ran: %v", err)
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
		if strings.Contains(line, "external_task_workers") {
			t.Errorf("the table migration 41 leaves differs from the model: %s", line)
		}
	}
	for _, column := range []string{"organization_id", "topic", "worker_id", "version", "last_seen_at"} {
		if !ledgerHasColumn(t, db, "external_task_workers", column) {
			t.Errorf("migration 41 left out external_task_workers.%s", column)
		}
	}
}
