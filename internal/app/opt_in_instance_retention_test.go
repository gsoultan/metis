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
	"github.com/gsoultan/metis/server/repositories/store/auditentry"
	"github.com/gsoultan/metis/server/repositories/store/incident"
	"github.com/gsoultan/metis/server/repositories/store/instancedeviation"
	"github.com/gsoultan/metis/server/repositories/store/notification"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/metis/server/repositories/store/servicecall"
	"github.com/gsoultan/metis/server/repositories/store/task"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm/runtime"
)

// Finished instances are removed only when an operator says how long to keep
// them, and then only the ones nothing still needs. Each goes with the rows
// that exist only to run it; its audit trail stays.
func TestTheInstanceSweepRemovesOnlyFinishedInstancesNothingStillNeeds(t *testing.T) {
	conn := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(conn)
	sse := impl.NewSSEObserver()
	a := &App{storm: conn, repo: repo, sse: sse,
		svc: services.NewServiceFacade(repo, impl.NewEventDispatcher(), sse, "retention-test", nil, nil, nil)}
	ctx := entities.WithSystemContext(t.Context())
	ex := testutils.Executor(conn)
	must := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	count := func(n int64, err error) int64 {
		t.Helper()
		must("count", err)
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
		changes := processinstance.MutateKey(id)
		changes.SetUpdatedAt(changed)
		if parent != uuid.Nil {
			changes.SetParentInstanceID(parent)
		}
		must("date the instance", changes.Update(t.Context(), ex))
		// Something of every kind the instance owns, and its audit trail.
		step := task.Create()
		step.SetProjectID(projectID)
		step.SetInstanceID(id)
		step.SetNodeID("review")
		step.SetName("Review")
		step.SetType(string(models.UserTask))
		step.SetStatus(string(models.TaskCompleted))
		step.SetCandidateUsers(runtime.JSON("[]"))
		step.SetCandidateGroups(runtime.JSON("[]"))
		step.SetPriority(0)
		step.SetVariables("")
		_, err := step.Insert(t.Context(), ex)
		must("seed a task", err)
		call := servicecall.Create()
		call.SetProjectID(projectID)
		call.SetInstanceID(id)
		call.SetNodeID("charge")
		call.SetStatus("completed")
		call.SetAttempts(1)
		call.SetIdempotencyKey(uuid.NewString())
		call.SetIterationID("")
		call.SetResponse(runtime.JSON("{}"))
		_, err = call.Insert(t.Context(), ex)
		must("seed a service call", err)
		note := notification.Create()
		note.SetProjectID(projectID)
		note.SetInstanceID(id)
		note.SetUserID("ana")
		note.SetTitle("Done")
		note.SetType("info")
		note.SetMessage("")
		note.SetIsRead(false)
		_, err = note.Insert(t.Context(), ex)
		must("seed a notification", err)
		entry := auditentry.Create()
		entry.SetProjectID(projectID)
		entry.SetInstanceID(id)
		entry.SetType("PROCESS_COMPLETED")
		entry.SetMessage("done")
		entry.SetData(runtime.JSON("{}"))
		_, err = entry.Insert(t.Context(), ex)
		must("seed an audit entry", err)
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
	deviation := instancedeviation.Create()
	deviation.SetProjectID(projectID)
	deviation.SetInstanceID(waived)
	deviation.SetKind("waive")
	deviation.SetScope("instance")
	deviation.SetOrigin("api")
	deviation.SetStatus("applied")
	deviation.SetActor("ada")
	deviation.SetBefore(runtime.JSON("{}"))
	deviation.SetAfter(runtime.JSON("{}"))
	deviation.SetDetails(runtime.JSON("{}"))
	deviation.SetRunID(uuid.Must(uuid.NewV7()))
	_, err := deviation.Insert(t.Context(), ex)
	must("seed a waiver", err)
	stuck := instance("completed long ago, incident open", models.ProcessCompleted, old, uuid.Nil)
	open := incident.Create()
	open.SetInstanceID(stuck)
	open.SetNodeID("charge")
	open.SetError("declined")
	open.SetStatus(string(models.IncidentOpen))
	_, err = open.Insert(t.Context(), ex)
	must("seed an incident", err)
	runningParent := instance("active parent", models.ProcessActive, old, uuid.Nil)
	instance("completed child of an active parent", models.ProcessCompleted, old, runningParent)
	finishedParent := instance("completed parent of an active child", models.ProcessCompleted, old, uuid.Nil)
	instance("active child", models.ProcessActive, old, finishedParent)
	everyEntry := func() int64 {
		t.Helper()
		return count(auditentry.New().Unordered().Count(t.Context(), ex))
	}
	audits := everyEntry()

	a.sweepRetention(ctx, time.Now(), resolveOptInRetention())
	if n := count(processinstance.New().Unordered().Count(t.Context(), ex)); n != int64(len(instances)) {
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
		left := count(processinstance.New().IDEq(id).Count(t.Context(), ex)) == 1
		if left == removed[name] {
			t.Errorf("the %s instance: left = %v, want %v", name, left, !removed[name])
		}
		owned := map[string]int64{
			"tasks":         count(task.New().InstanceIDEq(id).Count(t.Context(), ex)),
			"service_calls": count(servicecall.New().InstanceIDEq(id).Count(t.Context(), ex)),
			"notifications": count(notification.New().InstanceIDEq(id).Count(t.Context(), ex)),
		}
		for table, n := range owned {
			if (n == 1) != left {
				t.Errorf("the %s instance's row in %s: left = %v, want %v", name, table, n == 1, left)
			}
		}
	}
	if n := everyEntry(); n != audits {
		t.Errorf("the sweep left %d of %d audit entries; the audit trail is never swept", n, audits)
	}
}
