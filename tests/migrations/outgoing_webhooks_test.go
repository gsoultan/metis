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

// The outgoing webhooks reach a database two ways: an upgrading installation
// runs migration 40, whose DDL is frozen text, and a fresh one or a test schema
// gets the tables from the storm model. A queue the worker cannot read is
// events nobody is told about, so the two must describe the same tables.
func TestMigration40CreatesTheOutgoingWebhooksAsTheModelDescribesThem(t *testing.T) {
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

	// The database as an upgrading installation has it: no outgoing webhooks,
	// and migration 40 not yet run.
	for _, table := range []string{"outgoing_webhook_deliveries", "outgoing_webhooks"} {
		if err := db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS ` + table).Error; err != nil {
			t.Fatalf("remove %s: %v", table, err)
		}
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM schema_migrations WHERE version = ?`,
		migrations.OutgoingWebhooksMigration).Error; err != nil {
		t.Fatalf("forget that migration 40 ran: %v", err)
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
		if strings.Contains(line, "outgoing_webhook") {
			t.Errorf("the tables migration 40 leaves differ from the model: %s", line)
		}
	}
	for _, column := range []string{"project_id", "url", "events", "secret", "enabled"} {
		if !ledgerHasColumn(t, db, "outgoing_webhooks", column) {
			t.Errorf("migration 40 left out outgoing_webhooks.%s", column)
		}
	}
	for _, column := range []string{"webhook_id", "event", "body", "status", "attempts", "next_attempt_at", "test"} {
		if !ledgerHasColumn(t, db, "outgoing_webhook_deliveries", column) {
			t.Errorf("migration 40 left out outgoing_webhook_deliveries.%s", column)
		}
	}
}
