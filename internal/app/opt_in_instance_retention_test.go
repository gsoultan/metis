package app

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// Finished instances are removed only when an operator says how long to keep
// them, and then only the ones nothing still needs. Each goes with the rows
// that exist only to run it; its audit trail stays.
func TestTheInstanceSweepRemovesOnlyFinishedInstancesNothingStillNeeds(t *testing.T) {
	gormDB := testutils.SetupTestDB(t)
	conn := testutils.StormConn(gormDB)
	repo := repositories.NewRepository(conn)
	sse := impl.NewSSEObserver()
	a := &App{storm: conn, repo: repo, sse: sse,
		svc: services.NewServiceFacade(repo, impl.NewEventDispatcher(), sse, "retention-test", nil, nil, nil)}
	ctx := entities.WithSystemContext(t.Context())
	exec := func(stmt string, args ...any) {
		t.Helper()
		if err := gormDB.WithContext(t.Context()).Exec(stmt, args...).Error; err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	count := func(query string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := gormDB.WithContext(t.Context()).Raw(query, args...).Scan(&n).Error; err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return n
	}

	tenant, _, projectID := testutils.ScopedProject(t, repo)
	definitionID := uuid.Must(uuid.NewV7())
	if err := repo.Definition().Create(tenant, models.ProcessDefinitionModel{
		Base: models.Base{ID: models.FromUUID(definitionID)}, ProjectID: models.FromUUID(projectID),
		Key: "claims", Name: "Claims", Version: 1,
	}); err != nil {
		t.Fatalf("seed the definition: %v", err)
	}

	instances := map[string]uuid.UUID{}
	instance := func(name string, status models.ProcessStatus, changed time.Time, parent uuid.UUID) uuid.UUID {
		t.Helper()
		id := uuid.Must(uuid.NewV7())
		m := models.ProcessInstanceModel{
			Base: models.Base{ID: models.FromUUID(id)}, ProjectID: models.FromUUID(projectID),
			DefinitionID: models.FromUUID(definitionID), Status: status,
		}
		if _, err := repo.Process().Create(tenant, m); err != nil {
			t.Fatalf("seed the %s instance: %v", name, err)
		}
		exec(`UPDATE process_instances SET updated_at = ? WHERE id = ?`, changed, id)
		if parent != uuid.Nil {
			exec(`UPDATE process_instances SET parent_instance_id = ? WHERE id = ?`, parent, id)
		}
		// Something of every kind the instance owns, and its audit trail.
		exec(`INSERT INTO tasks (id, project_id, instance_id, node_id, name, status) VALUES (gen_random_uuid(), ?, ?, 'review', 'Review', 'completed')`, projectID, id)
		exec(`INSERT INTO service_calls (id, project_id, instance_id, node_id, status) VALUES (gen_random_uuid(), ?, ?, 'charge', 'completed')`, projectID, id)
		exec(`INSERT INTO notifications (id, project_id, instance_id, user_id, title) VALUES (gen_random_uuid(), ?, ?, 'ana', 'Done')`, projectID, id)
		exec(`INSERT INTO audit_logs (id, instance_id) VALUES (gen_random_uuid(), ?)`, id)
		instances[name] = id
		return id
	}

	old, lately := time.Now().Add(-longAgo), time.Now().Add(-time.Hour)
	instance("completed long ago", models.ProcessCompleted, old, uuid.Nil)
	instance("cancelled long ago", models.ProcessCancelled, old, uuid.Nil)
	instance("completed lately", models.ProcessCompleted, lately, uuid.Nil)
	instance("active", models.ProcessActive, old, uuid.Nil)
	instance("failed long ago", models.ProcessFailed, old, uuid.Nil)
	waived := instance("completed long ago, a step waived", models.ProcessCompleted, old, uuid.Nil)
	exec(`INSERT INTO instance_deviations (id, project_id, instance_id, kind, scope, origin, status, actor, before, after, details, run_id)
	      VALUES (gen_random_uuid(), ?, ?, 'waive', 'instance', 'api', 'applied', 'ada', '{}', '{}', '{}', gen_random_uuid())`, projectID, waived)
	stuck := instance("completed long ago, incident open", models.ProcessCompleted, old, uuid.Nil)
	exec(`INSERT INTO incidents (id, instance_id, node_id, error, status) VALUES (gen_random_uuid(), ?, 'charge', 'declined', ?)`,
		stuck, string(models.IncidentOpen))
	runningParent := instance("active parent", models.ProcessActive, old, uuid.Nil)
	instance("completed child of an active parent", models.ProcessCompleted, old, runningParent)
	finishedParent := instance("completed parent of an active child", models.ProcessCompleted, old, uuid.Nil)
	instance("active child", models.ProcessActive, old, finishedParent)
	audits := count(`SELECT count(*) FROM audit_logs`)

	a.sweepRetention(ctx, time.Now(), resolveOptInRetention())
	if n := count(`SELECT count(*) FROM process_instances`); n != int64(len(instances)) {
		t.Fatalf("an unconfigured sweep left %d of %d instances; want every one", n, len(instances))
	}

	t.Setenv(envRetainFinishedInstancesDays, "30")
	keep := resolveOptInRetention()
	if keep.finishedInstances != retainedFor {
		t.Fatalf("thirty days was read as %v", keep.finishedInstances)
	}
	a.sweepRetention(ctx, time.Now(), keep)

	removed := map[string]bool{"completed long ago": true, "cancelled long ago": true}
	for name, id := range instances {
		left := count(`SELECT count(*) FROM process_instances WHERE id = ?`, id) == 1
		if left == removed[name] {
			t.Errorf("the %s instance: left = %v, want %v", name, left, !removed[name])
		}
		for _, table := range []string{"tasks", "service_calls", "notifications"} {
			owned := count(`SELECT count(*) FROM `+table+` WHERE instance_id = ?`, id) == 1
			if owned != left {
				t.Errorf("the %s instance's row in %s: left = %v, want %v", name, table, owned, left)
			}
		}
	}
	if n := count(`SELECT count(*) FROM audit_logs`); n != audits {
		t.Errorf("the sweep left %d of %d audit entries; the audit trail is never swept", n, audits)
	}
}
