package migrations_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// Every webhook that exists when v2 signatures arrive was set up for the legacy
// scheme, because there was no other. Refusing that scheme outright would cut
// every partner off on the day of the upgrade, so migration 25 gives each one
// ninety days.
//
// The migration is only reachable from the old schema: a fresh install gets the
// column from the model and has no webhooks to open a window for. This puts
// webhooks back the way an upgrading installation has it — no column at all —
// with a webhook in it, and runs the migration at it.
func TestMigration25GivesExistingWebhooksNinetyDaysOfLegacySignatures(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()
	migrate := func() {
		t.Helper()
		if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
			t.Fatalf("run the migrations: %v", err)
		}
	}
	migrate()

	for _, stmt := range []string{
		`ALTER TABLE webhooks DROP COLUMN IF EXISTS legacy_signatures_until`,
		`DELETE FROM schema_migrations WHERE version = 25`,
	} {
		if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
			t.Fatalf("restore the old schema (%s): %v", stmt, err)
		}
	}
	existing := insertWebhookAsTheOldReleaseDid(t, db)

	before := time.Now()
	migrate()
	after := time.Now()

	until := legacyWindowOf(t, db, existing)
	if until == nil {
		t.Fatal("a webhook that existed before v2 was given no window: its sender is cut off on the day of the upgrade")
	}
	const ninetyDays = 90 * 24 * time.Hour
	if earliest, latest := before.Add(ninetyDays).Add(-time.Second), after.Add(ninetyDays).Add(time.Second); until.Before(earliest) || until.After(latest) {
		t.Errorf("the window closes at %v, want ninety days from the upgrade, between %v and %v", until, earliest, latest)
	}

	// A run repeated after a failure part-way does not push a window back.
	if err := db.WithContext(ctx).Exec(`DELETE FROM schema_migrations WHERE version = 25`).Error; err != nil {
		t.Fatalf("forget migration 25: %v", err)
	}
	migrate()
	if again := legacyWindowOf(t, db, existing); again == nil || !again.Equal(*until) {
		t.Errorf("running the migration again moved the window from %v to %v", until, again)
	}
}

// insertWebhookAsTheOldReleaseDid writes a webhook with only the columns the
// release before v2 knew about.
func insertWebhookAsTheOldReleaseDid(t *testing.T, db *gorm.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now()
	if err := db.WithContext(t.Context()).Exec(
		`INSERT INTO webhooks (id, created_at, updated_at, project_id, name, token, secret, signature_header, message_name, enabled)
		 VALUES (?, ?, ?, ?, 'Payments', ?, 'a-secret', 'X-Signature-256', 'payment.received', true)`,
		id, now, now, uuid.New(), "tok-"+id.String()[:8]).Error; err != nil {
		t.Fatalf("insert a webhook as the old release did: %v", err)
	}
	return id
}

func legacyWindowOf(t *testing.T, db *gorm.DB, id uuid.UUID) *time.Time {
	t.Helper()
	var until sql.NullTime
	if err := db.WithContext(t.Context()).Raw(
		`SELECT legacy_signatures_until FROM webhooks WHERE id = ?`, id).Row().Scan(&until); err != nil {
		t.Fatalf("read the webhook's legacy window: %v", err)
	}
	if !until.Valid {
		return nil
	}
	return &until.Time
}
