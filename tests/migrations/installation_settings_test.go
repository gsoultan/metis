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

// The saved settings reach a database the two ways the holds do (see
// migration 37's test). A setting the server cannot read back stops it
// starting, so the two must be of one table.
func TestMigration49CreatesTheInstallationSettingsAsTheModelDescribesThem(t *testing.T) {
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

	// The database as an upgrading installation has it: no settings,
	// and migration 49 not yet run.
	if err := db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS installation_settings`).Error; err != nil {
		t.Fatalf("remove the settings: %v", err)
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM schema_migrations WHERE version = ?`,
		migrations.InstallationSettingsMigration).Error; err != nil {
		t.Fatalf("forget that migration 49 ran: %v", err)
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
		if strings.Contains(line, "installation_settings") {
			t.Errorf("the table migration 49 leaves differs from the model: %s", line)
		}
	}
	for _, column := range []string{"name", "value", "sealed", "reason", "set_by", "set_by_id"} {
		if !ledgerHasColumn(t, db, "installation_settings", column) {
			t.Errorf("migration 49 left out installation_settings.%s", column)
		}
	}
}
