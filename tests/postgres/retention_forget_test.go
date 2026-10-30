package postgres_test

import (
	"testing"
	"time"

	"github.com/gsoultan/metis/server/repositories/store/deviationrequest"
	"github.com/gsoultan/metis/server/repositories/store/incident"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/storm/runtime"

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
	repo := repositories.NewRepository(db)
	ctx, _, project := testutils.ScopedProject(t, repo)
	definition := seedDefinition(t, db, project, 1)
	ex := testutils.Executor(db)
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
		ageInstance(ctx, t, db, id, 60*24*time.Hour)
		if parent != uuid.Nil {
			rewriteInstance(ctx, t, db, id, func(m *processinstance.Mut) { m.SetParentInstanceID(parent) })
		}
		instances[name] = id
		return id
	}

	runningParent := instance("running parent", models.ProcessActive, uuid.Nil)
	instance("child of a running parent", models.ProcessCompleted, runningParent)
	parent := instance("parent of a running child", models.ProcessCompleted, uuid.Nil)
	instance("running child", models.ProcessActive, parent)
	asked := instance("deviation asked for", models.ProcessCompleted, uuid.Nil)
	now := time.Now()
	if _, err := deviationrequest.InsertAll(t.Context(), ex, []deviationrequest.Row{{
		ID: uuid.Must(uuid.NewV7()), CreatedAt: now, UpdatedAt: now, ProjectID: project, Kind: "waive", Status: "pending",
		InstanceID: runtime.Null[[16]byte]{V: asked, Valid: true}, RequestedBy: "ada", RequestedByID: uuid.New(), Reason: "test",
		Command: runtime.JSON("{}"), Plan: runtime.JSON("{}"), Fingerprint: "f", ApprovedInstances: runtime.JSON("[]"),
		Outcome: runtime.JSON("{}"), ExpiresAt: now.Add(24 * time.Hour),
	}}); err != nil {
		t.Fatalf("seed the deviation request: %v", err)
	}
	faulted := instance("incident open, marked removed", models.ProcessCompleted, uuid.Nil)
	if _, err := incident.InsertAll(t.Context(), ex, []incident.Row{{
		ID: uuid.Must(uuid.NewV7()), CreatedAt: now, UpdatedAt: now, DeletedAt: runtime.Null[time.Time]{V: now, Valid: true},
		InstanceID: faulted, NodeID: "charge", Error: "declined", Status: string(models.IncidentOpen),
	}}); err != nil {
		t.Fatalf("seed the incident: %v", err)
	}
	marked := instance("marked removed", models.ProcessCompleted, uuid.Nil)
	if err := processinstance.Delete(t.Context(), ex, marked); err != nil {
		t.Fatalf("mark the instance removed: %v", err)
	}
	instance("plain", models.ProcessCompleted, uuid.Nil)

	if _, err := repo.Process().ArchiveFinished(entities.WithSystemContext(t.Context()), time.Now(), 30*24*time.Hour); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	gone := map[string]bool{"marked removed": true, "plain": true}
	for name, id := range instances {
		left, err := processinstance.New().WithDeleted().IDEq(id).Count(t.Context(), ex)
		if err != nil {
			t.Fatalf("read the %s instance: %v", name, err)
		}
		if (left == 0) != gone[name] {
			t.Errorf("the %s instance: left = %v, want %v", name, left == 1, !gone[name])
		}
	}
}
