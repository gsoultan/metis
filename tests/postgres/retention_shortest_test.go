package postgres_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// The sweep reaches back as far as the shortest period any process has NOW:
// each process's newest setting, not one it had before. A process cut from a
// hundred days to one loses an instance finished two days ago.
func TestTheSweepReadsEachProcesssNewestPeriod(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ctx, _, project := testutils.ScopedProject(t, repo)
	definition := seedDefinition(t, db, project, 1)
	var key string
	if err := db.Raw(`SELECT key FROM process_definitions WHERE id = ?`, definition).Scan(&key).Error; err != nil {
		t.Fatalf("read the key: %v", err)
	}
	for _, days := range []int{100, 1} {
		if _, err := repo.RetentionPeriod().Set(ctx, entities.RetentionPeriod{
			Project: &entities.Project{ID: project}, ProcessKey: key, Days: &days, Reason: "test", SetBy: "test",
		}); err != nil {
			t.Fatalf("set %d days: %v", days, err)
		}
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := repo.Process().Create(ctx, models.ProcessInstanceModel{
		Base: models.Base{ID: models.FromUUID(id)}, ProjectID: models.FromUUID(project),
		DefinitionID: models.FromUUID(definition), Status: models.ProcessCompleted,
	}); err != nil {
		t.Fatalf("seed an instance: %v", err)
	}
	if err := db.Exec(`UPDATE process_instances SET updated_at = now() - interval '2 days' WHERE id = ?`, id).Error; err != nil {
		t.Fatalf("age the instance: %v", err)
	}

	removed, err := repo.Process().ForgetFinished(entities.WithSystemContext(t.Context()), time.Now(), 0)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed %d instances, want the one past its process's newest period", removed)
	}
}
