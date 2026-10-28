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

// What keeps a finished instance past its period is read as the sweep finds
// it: a running parent or child, a deviation request, an incident left open
// even once it is marked removed. A marked instance that nothing keeps goes
// like a live one.
func TestTheSweepKeepsWhatAFinishedInstanceIsStillNeededFor(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	ctx, _, project := testutils.ScopedProject(t, repo)
	definition := seedDefinition(t, db, project, 1)
	exec := func(sql string, args ...any) {
		t.Helper()
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	instances := map[string]uuid.UUID{}
	instance := func(name string, status models.ProcessStatus, parent uuid.UUID) uuid.UUID {
		t.Helper()
		id := uuid.Must(uuid.NewV7())
		if _, err := repo.Process().Create(ctx, models.ProcessInstanceModel{
			Base: models.Base{ID: models.FromUUID(id)}, ProjectID: models.FromUUID(project),
			DefinitionID: models.FromUUID(definition), Status: status,
		}); err != nil {
			t.Fatalf("seed the %s instance: %v", name, err)
		}
		exec(`UPDATE process_instances SET updated_at = now() - interval '60 days' WHERE id = ?`, id)
		if parent != uuid.Nil {
			exec(`UPDATE process_instances SET parent_instance_id = ? WHERE id = ?`, parent, id)
		}
		instances[name] = id
		return id
	}

	runningParent := instance("running parent", models.ProcessActive, uuid.Nil)
	instance("child of a running parent", models.ProcessCompleted, runningParent)
	parent := instance("parent of a running child", models.ProcessCompleted, uuid.Nil)
	instance("running child", models.ProcessActive, parent)
	asked := instance("deviation asked for", models.ProcessCompleted, uuid.Nil)
	exec(`INSERT INTO deviation_requests (id, project_id, kind, status, instance_id, requested_by, requested_by_id, reason, command, plan, fingerprint, approved_instances, outcome, expires_at)
	      VALUES (gen_random_uuid(), ?, 'waive', 'pending', ?, 'ada', gen_random_uuid(), 'test', '{}', '{}', 'f', '[]', '{}', now() + interval '1 day')`, project, asked)
	faulted := instance("incident open, marked removed", models.ProcessCompleted, uuid.Nil)
	exec(`INSERT INTO incidents (id, instance_id, node_id, error, status, deleted_at) VALUES (gen_random_uuid(), ?, 'charge', 'declined', ?, now())`,
		faulted, string(models.IncidentOpen))
	marked := instance("marked removed", models.ProcessCompleted, uuid.Nil)
	exec(`UPDATE process_instances SET deleted_at = now() WHERE id = ?`, marked)
	instance("plain", models.ProcessCompleted, uuid.Nil)

	if _, err := repo.Process().ForgetFinished(entities.WithSystemContext(t.Context()), time.Now(), 30*24*time.Hour); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	gone := map[string]bool{"marked removed": true, "plain": true}
	for name, id := range instances {
		var left int64
		if err := db.Raw(`SELECT count(*) FROM process_instances WHERE id = ?`, id).Scan(&left).Error; err != nil {
			t.Fatalf("read the %s instance: %v", name, err)
		}
		if (left == 0) != gone[name] {
			t.Errorf("the %s instance: left = %v, want %v", name, left == 1, !gone[name])
		}
	}
}
