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

// The task links reach a database the two ways the holds do (see
// migration 37's test). A link the server cannot read back is a task
// nobody can reach from their mail, so the two must be of one table.
func TestMigration47CreatesTheTaskLinksAsTheModelDescribesThem(t *testing.T) {
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

	// The database as an upgrading installation has it: no links,
	// and migration 47 not yet run.
	if err := db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS task_links`).Error; err != nil {
		t.Fatalf("remove the links: %v", err)
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM schema_migrations WHERE version = ?`,
		migrations.TaskLinksMigration).Error; err != nil {
		t.Fatalf("forget that migration 47 ran: %v", err)
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
		if strings.Contains(line, "task_links") {
			t.Errorf("the table migration 47 leaves differs from the model: %s", line)
		}
	}
	for _, column := range []string{"project_id", "task_id", "participant_id", "token_hash", "expires_at", "used_at", "voided_at"} {
		if !ledgerHasColumn(t, db, "task_links", column) {
			t.Errorf("migration 47 left out task_links.%s", column)
		}
	}
}
