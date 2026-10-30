package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
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

// Finished instances are archived once past their period, and only the ones
// nothing still needs. Each leaves the live tables with the rows that exist
// only to run it, and its record keeps all of them and its audit trail; the
// entries naming somebody who acted stay in the live trail too.
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
		must("date the instance", changes.Update(tenant, ex))
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
		_, err := step.Insert(tenant, ex)
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
		_, err = call.Insert(tenant, ex)
		must("seed a service call", err)
		note := notification.Create()
		note.SetProjectID(projectID)
		note.SetInstanceID(id)
		note.SetUserID("ana")
		note.SetTitle("Done")
		note.SetType("info")
		note.SetMessage("")
		note.SetIsRead(false)
		_, err = note.Insert(tenant, ex)
		must("seed a notification", err)
		entry := auditentry.Create()
		entry.SetProjectID(projectID)
		entry.SetInstanceID(id)
		entry.SetType("PROCESS_COMPLETED")
		entry.SetMessage("done")
		entry.SetData(runtime.JSON("{}"))
		_, err = entry.Insert(tenant, ex)
		must("seed an audit entry", err)
		act := auditentry.Create()
		act.SetProjectID(projectID)
		act.SetInstanceID(id)
		act.SetType("TASK_REASSIGNED")
		act.SetMessage("reassigned")
		act.SetActor("ada")
		act.SetData(runtime.JSON("{}"))
		_, err = act.Insert(tenant, ex)
		must("seed an audit entry somebody's act wrote", err)
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
	t.Setenv(envRetainFinishedInstancesDays, "0")
	a.sweepRetention(ctx, time.Now(), resolveOptInRetention())
	if n := count(processinstance.New().Unordered().Count(t.Context(), ex)); n != int64(len(instances)) {
		t.Fatalf("a sweep with the period at 0 left %d of %d instances; want every one", n, len(instances))
	}

	t.Setenv(envRetainFinishedInstancesDays, "30")
	keep := resolveOptInRetention()
	if keep.finishedInstances != retainedFor {
		t.Fatalf("thirty days was read as %v", keep.finishedInstances)
	}
	a.sweepRetention(ctx, time.Now(), keep)

	removed := map[string]bool{"completed long ago": true, "cancelled long ago": true}
	for name, id := range instances {
		expectArchived(t, a, tenant, name, id, removed[name])
		engine := count(auditentry.New().Where(auditentry.InstanceID.Eq(id), auditentry.Actor.IsNull()).Count(t.Context(), ex))
		if (engine == 1) == removed[name] {
			t.Errorf("the %s instance's engine audit entry: left = %v, want %v", name, engine == 1, !removed[name])
		}
		if acts := count(auditentry.New().Where(auditentry.InstanceID.Eq(id), auditentry.Actor.IsNotNull()).Count(t.Context(), ex)); acts != 1 {
			t.Errorf("the %s instance has %d live audit entries naming somebody, want 1: the audit log keeps who did what", name, acts)
		}
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
}

// expectArchived checks that an instance is in the archive, with everything
// it owned and its whole trail, or that it is not there at all.
func expectArchived(t *testing.T, a *App, tenant context.Context, name string, id uuid.UUID, want bool) {
	t.Helper()
	summary, record, err := a.repo.Archive().Get(tenant, id)
	if !want {
		if !errors.Is(err, apierr.ErrNotFound) {
			t.Errorf("the %s instance reads from the archive with %v, want not found", name, err)
		}
		return
	}
	if err != nil {
		t.Errorf("open the %s instance's archive record: %v", name, err)
		return
	}
	if summary.ProcessKey != "claims" || summary.ProcessName != "Claims" || summary.ProcessVersion != 1 ||
		uuid.UUID(record.Instance.ID) != id {
		t.Errorf("the %s instance's archive summary is %+v", name, summary)
	}
	if len(record.Tasks) != 1 || len(record.ServiceCalls) != 1 || len(record.Notifications) != 1 || len(record.AuditTrail) != 2 {
		t.Errorf("the %s instance's archive record holds %d tasks, %d service calls, %d notifications and %d audit entries; want 1, 1, 1 and 2",
			name, len(record.Tasks), len(record.ServiceCalls), len(record.Notifications), len(record.AuditTrail))
	}
}
