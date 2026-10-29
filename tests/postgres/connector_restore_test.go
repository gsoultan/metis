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
	"github.com/gsoultan/metis/server/repositories/store/connector"
	"github.com/gsoultan/metis/tests/testutils"
)

// Restoring a connector by its key brings back the entry removed last under
// that key, and only a removed one.
func TestRestoringAConnectorBringsBackTheOneRemovedLast(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ctx := entities.WithSystemContext(t.Context())
	connectors := repo.Connector()
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
	ex := testutils.Executor(db)
	last, found, err := connector.New().WithDeleted().IDEq(uuid.UUID(second.ID)).One(ctx, ex)
	if err != nil || !found {
		t.Fatalf("read the second mark: %v (found=%v)", err, found)
	}
	// A marked row is written only by restoring it: mark it again, earlier.
	if err := connector.Restore(ctx, ex, uuid.UUID(first.ID)); err != nil {
		t.Fatalf("age the first mark: %v", err)
	}
	earlier := connector.MutateKey(uuid.UUID(first.ID))
	earlier.SetDeletedAt(last.DeletedAt.V.Add(-time.Second))
	if err := earlier.Update(ctx, ex); err != nil {
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
