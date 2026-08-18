package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"github.com/gsoultan/gobpm/tests/testutils"
	"gorm.io/gorm"
)

// A failed statement poisons its PostgreSQL transaction: every later statement
// on that connection is refused until it rolls back. Reusing an enclosing
// transaction for work the caller intends to retry therefore turns one
// recoverable conflict into a dead transaction, and the retry loop returns the
// same error until it gives up.
//
// UnitOfWork.Attempt takes a savepoint so a failed try rolls back to just before
// itself. This is what lets a deployment — which creates its definitions inside
// one transaction — recover from losing a version race.
func TestAttemptLeavesAnEnclosingTransactionUsable(t *testing.T) {
	db := testutils.SetupPostgresDB(t, 4)
	repo := repositories.NewRepository(db)
	uow := repo.UnitOfWork()

	project := models.UUID(uuid.New())
	seeded := models.UUID(uuid.New())

	err := uow.Do(t.Context(), func(txCtx context.Context) error {
		if err := createDefinitionRow(txCtx, repo, seeded, project, "nested", 1); err != nil {
			return err
		}

		// Lose the race: claim a version that is already taken.
		clash := uow.Attempt(txCtx, func(inner context.Context) error {
			return createDefinitionRow(inner, repo, models.UUID(uuid.New()), project, "nested", 1)
		})
		if clash == nil {
			return errors.New("a duplicate version was accepted inside the transaction")
		}
		if !errors.Is(clash, gorm.ErrDuplicatedKey) {
			return errors.New("the duplicate did not surface as gorm.ErrDuplicatedKey: " + clash.Error())
		}

		// The retry the allocator would make. Without the savepoint this is
		// where PostgreSQL refuses everything with "current transaction is
		// aborted".
		return uow.Attempt(txCtx, func(inner context.Context) error {
			return createDefinitionRow(inner, repo, models.UUID(uuid.New()), project, "nested", 2)
		})
	})
	if err != nil {
		t.Fatalf("the transaction did not survive a failed attempt: %v", err)
	}

	var versions []int
	if err := db.Model(&models.ProcessDefinitionModel{}).
		Where("project_id = ?", project).
		Order("version").
		Pluck("version", &versions).Error; err != nil {
		t.Fatalf("read versions: %v", err)
	}
	if len(versions) != 2 || versions[0] != 1 || versions[1] != 2 {
		t.Errorf("versions = %v, want [1 2] — the failed attempt must leave nothing behind", versions)
	}
}

func createDefinitionRow(ctx context.Context, repo repositories.Repository, id, project models.UUID, key string, version int) error {
	return repo.Definition().Create(ctx, models.ProcessDefinitionModel{
		Base:      models.Base{ID: id, CreatedAt: time.Now()},
		ProjectID: project,
		Key:       key,
		Name:      key,
		Version:   version,
	})
}
