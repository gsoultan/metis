package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// An update reaches only an instance the caller may see, and one that is not
// there is reported as not found, whoever asks. The update no longer reads
// the row first, so the scope and the missing row are both the write's to
// answer.
func TestAnInstanceUpdateStaysInsideTheCallersScope(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ours, _, project := testutils.ScopedProject(t, repo)
	theirs, _, _ := testutils.ScopedProject(t, repo)
	system := entities.WithSystemContext(t.Context())

	definition := testutils.SeedDefinition(t, db, project, "k")
	instance := models.ProcessInstanceModel{
		Base: models.Base{ID: models.FromUUID(uuid.Must(uuid.NewV7()))}, ProjectID: models.FromUUID(project),
		DefinitionID: models.FromUUID(definition), Status: models.ProcessActive,
		Variables: map[string]any{"amount": float64(10)},
	}
	if _, err := repo.Process().Create(ours, instance); err != nil {
		t.Fatalf("seed the instance: %v", err)
	}

	changed := instance
	changed.Status = models.ProcessCompleted
	changed.Variables = map[string]any{"amount": float64(20)}
	if err := repo.Process().Update(theirs, changed); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("another organization's update: %v, want not found", err)
	}
	stored, err := repo.Process().Get(system, uuid.UUID(instance.ID))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored.Status != models.ProcessActive || stored.Variables["amount"] != float64(10) {
		t.Fatalf("another organization's update was written: %s %v", stored.Status, stored.Variables)
	}

	if err := repo.Process().Update(ours, changed); err != nil {
		t.Fatalf("the owner's update: %v", err)
	}
	stored, err = repo.Process().Get(system, uuid.UUID(instance.ID))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored.Status != models.ProcessCompleted || stored.Variables["amount"] != float64(20) {
		t.Fatalf("the owner's update was not written: %s %v", stored.Status, stored.Variables)
	}

	missing := changed
	missing.ID = models.FromUUID(uuid.Must(uuid.NewV7()))
	for name, ctx := range map[string]context.Context{"scoped": ours, "system": system} {
		if err := repo.Process().Update(ctx, missing); !errors.Is(err, apierr.ErrNotFound) {
			t.Errorf("%s update of an instance that is not there: %v, want not found", name, err)
		}
	}
}
