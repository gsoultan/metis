package migrations_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/models"
	"gorm.io/gorm"
)

const versionIndex = "ux_process_definitions_version"

// An installation that ran the racy version allocator can already hold two
// definitions claiming the same version, and the unique index cannot be built
// over them. The migration has to repair the data before it constrains it —
// otherwise the upgrade fails on exactly the deployments that needed it.
//
// The repair renumbers rather than deletes: a process definition is a business
// artifact, running instances point at it by ID, and a duplicate version number
// is a labelling mistake, not grounds for destroying one of the two.
func TestDuplicateVersionsAreRenumberedNotDeleted(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		// The helper migrates from the models, so the constraint is already
		// there. Drop it to reproduce a database from before it existed.
		if db.Migrator().HasIndex(&models.ProcessDefinitionModel{}, versionIndex) {
			if err := db.Migrator().DropIndex(&models.ProcessDefinitionModel{}, versionIndex); err != nil {
				t.Fatalf("drop index: %v", err)
			}
		}

		project := models.UUID(uuid.New())
		base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

		// first and second both claim version 1, as two concurrent deploys did.
		// third is an ordinary later version, and must keep its number.
		first := seedDefinition(t, db, project, "orders", 1, base)
		second := seedDefinition(t, db, project, "orders", 1, base.Add(time.Second))
		third := seedDefinition(t, db, project, "orders", 2, base.Add(2*time.Second))

		// A different key in the same project has its own series and must not be
		// disturbed by the repair.
		otherKey := seedDefinition(t, db, project, "refunds", 1, base)

		// A soft-deleted row still occupies its number: reusing it would be
		// refused by the index the migration is about to add.
		deleted := seedDefinition(t, db, project, "orders", 3, base.Add(3*time.Second))
		if err := db.Model(&models.ProcessDefinitionModel{}).
			Where("id = ?", deleted).
			Update("deleted_at", base.Add(4*time.Second)).Error; err != nil {
			t.Fatalf("soft delete: %v", err)
		}

		if _, err := migrations.Run(t.Context(), db, migrations.Schema(models.MigrationModels())); err != nil {
			t.Fatalf("run migrations: %v", err)
		}

		// The earliest arrival keeps the contested number, so whichever
		// definition was deployed as version 1 first is still version 1.
		if got := versionOf(t, db, first); got != 1 {
			t.Errorf("the first version 1 became version %d; it should keep its number", got)
		}
		if got := versionOf(t, db, second); got == 1 {
			t.Error("both definitions still claim version 1")
		}
		if got := versionOf(t, db, third); got != 2 {
			t.Errorf("an unambiguous version was renumbered to %d", got)
		}
		if got := versionOf(t, db, otherKey); got != 1 {
			t.Errorf("a different key was renumbered to %d", got)
		}

		// Nothing was thrown away.
		for _, id := range []models.UUID{first, second, third, otherKey} {
			if !definitionExists(t, db, id) {
				t.Errorf("definition %s was deleted by the repair", uuid.UUID(id))
			}
		}
		if versionOf(t, db, second) == versionOf(t, db, deleted) {
			t.Error("the renumbered row took a version a soft-deleted row still holds")
		}

		if !db.Migrator().HasIndex(&models.ProcessDefinitionModel{}, versionIndex) {
			t.Fatal("the unique index was not created")
		}
	})
}

// Once the constraint exists, a second row claiming a taken version is refused
// by the database rather than accepted — and refused in a form the version
// allocator can recognise on every engine.
func TestDuplicateVersionIsRefusedAsADuplicateKey(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		if _, err := migrations.Run(t.Context(), db, migrations.Schema(models.MigrationModels())); err != nil {
			t.Fatalf("run migrations: %v", err)
		}

		project := models.UUID(uuid.New())
		seedDefinition(t, db, project, "invoices", 1, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))

		err := db.Create(&models.ProcessDefinitionModel{
			Base:      models.Base{ID: models.UUID(uuid.New()), CreatedAt: time.Now()},
			ProjectID: project,
			Key:       "invoices",
			Version:   1,
		}).Error
		if err == nil {
			t.Fatal("a second definition claimed version 1 and the database allowed it")
		}
		if !errors.Is(err, gorm.ErrDuplicatedKey) {
			t.Fatalf("error = %v, want gorm.ErrDuplicatedKey — the allocator retries on that and nothing else", err)
		}

		// The same key in another project is a separate series.
		if err := db.Create(&models.ProcessDefinitionModel{
			Base:      models.Base{ID: models.UUID(uuid.New()), CreatedAt: time.Now()},
			ProjectID: models.UUID(uuid.New()),
			Key:       "invoices",
			Version:   1,
		}).Error; err != nil {
			t.Fatalf("another project's version 1 was refused: %v", err)
		}
	})
}

func seedDefinition(t *testing.T, db *gorm.DB, project models.UUID, key string, version int, createdAt time.Time) models.UUID {
	t.Helper()
	id := models.UUID(uuid.New())
	if err := db.Create(&models.ProcessDefinitionModel{
		Base:      models.Base{ID: id, CreatedAt: createdAt},
		ProjectID: project,
		Key:       key,
		Name:      key,
		Version:   version,
	}).Error; err != nil {
		t.Fatalf("seed %s v%d: %v", key, version, err)
	}
	return id
}

func versionOf(t *testing.T, db *gorm.DB, id models.UUID) int {
	t.Helper()
	var m models.ProcessDefinitionModel
	if err := db.Unscoped().First(&m, "id = ?", id).Error; err != nil {
		t.Fatalf("read %s: %v", uuid.UUID(id), err)
	}
	return m.Version
}

func definitionExists(t *testing.T, db *gorm.DB, id models.UUID) bool {
	t.Helper()
	var count int64
	if err := db.Model(&models.ProcessDefinitionModel{}).Where("id = ?", id).Count(&count).Error; err != nil {
		t.Fatalf("count %s: %v", uuid.UUID(id), err)
	}
	return count > 0
}
