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

// The retention periods reach a database the two ways the holds do (see
// migration 38's test). A period the sweep reads wrongly removes a process's
// instances early, so the two descriptions must be of one table.
func TestMigration38CreatesTheRetentionPeriodsAsTheModelDescribesThem(t *testing.T) {
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

	// The database as an upgrading installation has it: no periods,
	// and migration 38 not yet run.
	if err := db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS retention_periods`).Error; err != nil {
		t.Fatalf("remove the periods: %v", err)
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM schema_migrations WHERE version = ?`,
		migrations.RetentionPeriodsMigration).Error; err != nil {
		t.Fatalf("forget that migration 38 ran: %v", err)
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
		if strings.Contains(line, "retention_periods") {
			t.Errorf("the table migration 38 leaves differs from the model: %s", line)
		}
	}
	for _, column := range []string{"project_id", "process_key", "days", "reason", "set_by", "set_by_id"} {
		if !ledgerHasColumn(t, db, "retention_periods", column) {
			t.Errorf("migration 38 left out retention_periods.%s", column)
		}
	}
}
