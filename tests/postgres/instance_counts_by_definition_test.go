package postgres_test

import (
	"github.com/gsoultan/metis/server/repositories/db"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// The version history shows each version's running, suspended and total
// instances, counted once across every version asked about. Another
// organization's instances of the same definition and soft-deleted ones are
// not counted.
func TestEachVersionCountsItsOwnInstancesByState(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ours, _, project := testutils.ScopedProject(t, repo)
	theirs, _, theirProject := testutils.ScopedProject(t, repo)

	first, second, idle := seedDefinition(t, db, project, 1), seedDefinition(t, db, project, 2), seedDefinition(t, db, project, 3)
	seed := func(projectID, definition uuid.UUID, statuses ...models.ProcessStatus) []uuid.UUID {
		t.Helper()
		var ids []uuid.UUID
		for _, status := range statuses {
			id := uuid.Must(uuid.NewV7())
			c := ours
			if projectID == theirProject {
				c = theirs
			}
			if _, err := repo.Process().Create(c, models.ProcessInstanceModel{
				Base: models.Base{ID: models.FromUUID(id)}, ProjectID: models.FromUUID(projectID),
				DefinitionID: models.FromUUID(definition), Status: status,
			}); err != nil {
				t.Fatalf("seed an instance: %v", err)
			}
			ids = append(ids, id)
		}
		return ids
	}
	seed(project, first, models.ProcessActive, models.ProcessActive, models.ProcessSuspended, models.ProcessCompleted)
	deleted := seed(project, second, models.ProcessActive, models.ProcessFailed)
	if err := db.Model(&models.ProcessInstanceModel{}).Where("id = ?", models.FromUUID(deleted[0])).
		Update("deleted_at", gorm.Expr("now()")).Error; err != nil {
		t.Fatalf("delete an instance: %v", err)
	}
	theirDefinition := seedDefinition(t, db, theirProject, 1)
	seed(theirProject, theirDefinition, models.ProcessActive)

	counts, err := repo.Process().CountInstancesByDefinitions(ours, []uuid.UUID{first, second, idle, theirDefinition})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	want := map[uuid.UUID]contracts.DefinitionInstanceCount{
		first:  {Running: 2, Suspended: 1, Total: 4},
		second: {Total: 1},
	}
	if len(counts) != len(want) {
		t.Errorf("counted %d versions, want %d: %v", len(counts), len(want), counts)
	}
	for id, w := range want {
		if counts[id] != w {
			t.Errorf("version %s: %+v, want %+v", id, counts[id], w)
		}
	}
}

func seedDefinition(t *testing.T, db *db.Conn, project uuid.UUID, version int) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	if err := db.Create(&models.ProcessDefinitionModel{
		Base: models.Base{ID: models.FromUUID(id)}, ProjectID: models.FromUUID(project),
		Key: "counted-" + id.String()[:8], Name: "counted", Version: version,
	}).Error; err != nil {
		t.Fatalf("seed a definition: %v", err)
	}
	return id
}
