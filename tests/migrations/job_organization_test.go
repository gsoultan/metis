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

// An upgrading installation gets the jobs' organization from migration 44, and
// a fresh one from the model. The claim reads through the index, so both must
// leave the column and the index the model describes.
func TestMigration44GivesJobsTheOrganizationTheModelDescribes(t *testing.T) {
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

	// The table as an upgrading installation has it: no organization, and
	// migration 44 not yet run.
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS ix_jobs_claim_by_organization`,
		`ALTER TABLE jobs DROP COLUMN IF EXISTS organization_id`,
	} {
		if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM schema_migrations WHERE version = ?`,
		migrations.JobOrganizationMigration).Error; err != nil {
		t.Fatalf("forget that migration 44 ran: %v", err)
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
		if strings.Contains(line, "jobs") && (strings.Contains(line, "organization_id") || strings.Contains(line, "ix_jobs_claim_by_organization")) {
			t.Errorf("the column or index migration 44 leaves differs from the model: %s", line)
		}
	}
	if !ledgerHasColumn(t, db, "jobs", "organization_id") {
		t.Error("migration 44 left out jobs.organization_id")
	}
}
