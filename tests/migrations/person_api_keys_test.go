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

// The personal keys reach a database the two ways the holds do (see
// migration 37's test). A key the server cannot read back is a system that
// can no longer act for its person, so the two must be of one table.
func TestMigration48CreatesThePersonalKeysAsTheModelDescribesThem(t *testing.T) {
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

	// The database as an upgrading installation has it: no keys,
	// and migration 48 not yet run.
	if err := db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS person_api_keys`).Error; err != nil {
		t.Fatalf("remove the keys: %v", err)
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM schema_migrations WHERE version = ?`,
		migrations.PersonAPIKeysMigration).Error; err != nil {
		t.Fatalf("forget that migration 48 ran: %v", err)
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
		if strings.Contains(line, "person_api_keys") {
			t.Errorf("the table migration 48 leaves differs from the model: %s", line)
		}
	}
	for _, column := range []string{"participant_id", "name", "prefix", "token_hash", "last_used_at", "revoked_at"} {
		if !ledgerHasColumn(t, db, "person_api_keys", column) {
			t.Errorf("migration 48 left out person_api_keys.%s", column)
		}
	}
}
