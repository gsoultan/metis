package migrations_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// A webhook's secret signs its deliveries, and before migration 42 it was
// stored in the clear. The migration seals what an older release left behind,
// deleted webhooks' too, and leaves what is already sealed alone.
func TestMigration42SealsTheWebhookSecretsAlreadyStored(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()
	migrate := func() {
		t.Helper()
		if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
			t.Fatalf("run the migrations: %v", err)
		}
	}
	forget := func() {
		t.Helper()
		stmt := `DELETE FROM schema_migrations WHERE version = ?`
		if err := db.WithContext(ctx).Exec(stmt, migrations.WebhookSecretsMigration).Error; err != nil {
			t.Fatalf("forget migration 42: %v", err)
		}
	}
	migrate()
	forget()

	live := insertWebhookAsTheOldReleaseDid(t, db)
	deleted := insertWebhookAsTheOldReleaseDid(t, db)
	if err := db.WithContext(ctx).Exec(`UPDATE webhooks SET deleted_at = now() WHERE id = ?`, deleted).Error; err != nil {
		t.Fatalf("delete a webhook: %v", err)
	}

	migrate()

	sealed := map[uuid.UUID]string{}
	for _, id := range []uuid.UUID{live, deleted} {
		stored := storedSecretOf(t, db, id)
		if !crypto.IsCiphertext(stored) {
			t.Fatalf("webhook %s still holds its secret in the clear: %q", id, stored)
		}
		plain, err := crypto.Decrypt(stored)
		if err != nil {
			t.Fatalf("the sealed secret of %s does not open: %v", id, err)
		}
		if plain != "a-secret" {
			t.Errorf("webhook %s's secret opens to %q, want the one it had", id, plain)
		}
		sealed[id] = stored
	}

	// A repeated run does not seal a sealed secret a second time.
	forget()
	migrate()
	for id, before := range sealed {
		if after := storedSecretOf(t, db, id); after != before {
			t.Errorf("running the migration again changed webhook %s's secret from %q to %q", id, before, after)
		}
	}
}

func storedSecretOf(t *testing.T, db *gorm.DB, id uuid.UUID) string {
	t.Helper()
	var secret string
	if err := db.WithContext(t.Context()).Raw(
		`SELECT secret FROM webhooks WHERE id = ?`, id).Row().Scan(&secret); err != nil {
		t.Fatalf("read the webhook's secret: %v", err)
	}
	return secret
}
