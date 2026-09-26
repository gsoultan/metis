package instancemigration

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// Work parked on a timer is a job, and a migration rewrites the job's version
// and node along with the instance's. The job repository's Update wrote the
// node and never the version, so after a migration the job still named the old
// one: when the timer came due it looked for the new node in the old
// definition, found nothing there, and dismissed itself as a timer whose branch
// had moved on. The migrated instance waited on it for good.
func waitThenWork(projectID uuid.UUID, timerID, workID string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "cooling-off",
		Name:    "Cooling-off period",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: timerID, Type: entities.IntermediateCatchEvent, Properties: map[string]any{"timer_duration": "PT1H"}},
			{ID: workID, Type: entities.UserTask, Name: "Carry on", Assignee: "ada"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: timerID},
			{ID: "f2", SourceRef: timerID, TargetRef: workID},
			{ID: "f3", SourceRef: workID, TargetRef: "end"},
		},
	}
}

func TestATimerMovedByAMigrationFiresOnTheNewVersion(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), observersimpl.NewSSEObserver(),
		"migration-timer-test", nil, nil, nil, func(*gorm.DB) {})
	ctx, _, projectID := testutils.ScopedProject(t, repo)

	v1, err := svc.CreateDefinition(ctx, waitThenWork(projectID, "wait", "oldWork"))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	instanceID, err := svc.StartProcess(ctx, projectID, "cooling-off", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	v2, err := svc.CreateDefinition(ctx, waitThenWork(projectID, "pause", "newWork"))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	if err := svc.MigrateInstances(ctx, v1, v2, map[string]string{"wait": "pause", "oldWork": "newWork"}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// The hour passes.
	jobs, err := repo.Job().ListByInstance(ctx, instanceID)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("expected the timer's one job: jobs=%d err=%v", len(jobs), err)
	}
	job := jobs[0]
	job.NextRunAt = time.Now().Add(-time.Minute)
	if err := repo.Job().Update(ctx, job); err != nil {
		t.Fatalf("bring the timer due: %v", err)
	}
	if err := svc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	instance, err := svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	if len(instance.GetTokensByNode(&entities.Node{ID: "newWork"})) != 1 {
		t.Fatalf("the migrated timer did not carry the instance on to the new version's next step; tokens: %+v", instance.Tokens)
	}
}
