package postgres_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// Restoring a connector by its key brings back the entry removed last under
// that key, and only a removed one.
func TestRestoringAConnectorBringsBackTheOneRemovedLast(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ctx := entities.WithSystemContext(t.Context())
	connectors := repo.Connector()
	// The key is unique among the live rows, as the model declares it; the
	// harness still builds this table through GORM, which made it unique
	// over every row.
	if err := db.Exec(`DROP INDEX idx_connectors_key; CREATE UNIQUE INDEX uq_connectors_key ON connectors (key) WHERE deleted_at IS NULL`).Error; err != nil {
		t.Fatalf("scope the key to live rows: %v", err)
	}

	if _, err := connectors.RestoreByKey(ctx, "ledger"); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("restoring a key never used: %v, want not found", err)
	}
	first, err := connectors.Create(ctx, models.Connector{Key: "ledger", Name: "Ledger v1", Type: "http"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := connectors.RestoreByKey(ctx, "ledger"); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("restoring a live connector: %v, want not found", err)
	}
	if err := connectors.Delete(ctx, uuid.UUID(first.ID)); err != nil {
		t.Fatalf("delete v1: %v", err)
	}
	second, err := connectors.Create(ctx, models.Connector{Key: "ledger", Name: "Ledger v2", Type: "http"})
	if err != nil {
		t.Fatalf("create again: %v", err)
	}
	if err := connectors.Delete(ctx, uuid.UUID(second.ID)); err != nil {
		t.Fatalf("delete v2: %v", err)
	}
	// The marks are a second apart, whatever the clock did between the calls.
	if err := db.Exec(`UPDATE connectors SET deleted_at = deleted_at - interval '1 second' WHERE id = ?`, uuid.UUID(first.ID)).Error; err != nil {
		t.Fatalf("age the first mark: %v", err)
	}

	restored, err := connectors.RestoreByKey(ctx, "ledger")
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.ID != second.ID || restored.Name != "Ledger v2" {
		t.Errorf("restored %s (%s), want the one removed last", restored.ID, restored.Name)
	}
	if restored.UpdatedAt.Before(time.Now().Add(-time.Minute)) {
		t.Errorf("the restore did not touch updated_at: %v", restored.UpdatedAt)
	}
	live, err := connectors.GetByKey(ctx, "ledger")
	if err != nil || live.ID != second.ID {
		t.Errorf("the key reads %v (%v), want the restored connector", live.ID, err)
	}
}
