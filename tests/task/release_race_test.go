package task_test

import (
	"context"
	"sync"
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

// A release, or an edit, racing a completion.
//
// UnclaimTask and UpdateTask read the task without a lock and wrote the whole
// row back. One that read the task while it was being completed waited for
// the completion to commit, then wrote its own copy over it: the finished task
// was open again, and completing it again would run everything after it a
// second time.
func TestAReleaseOrAnEditRacingACompletionDoesNotReopenTheTask(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), observersimpl.NewSSEObserver(),
		"release-race-test", nil, nil, nil, func(*gorm.DB) {})
	ctx, _, projectID := testutils.ScopedProject(t, repo)

	if _, err := svc.CreateDefinition(ctx, &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "open-review",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "end"},
		},
	}); err != nil {
		t.Fatalf("deploy: %v", err)
	}

	racers := map[string]func(ctx context.Context, id uuid.UUID) error{
		"release": svc.UnclaimTask,
		"edit": func(ctx context.Context, id uuid.UUID) error {
			return svc.UpdateTask(ctx, entities.Task{ID: id, Name: "Review, renamed", Priority: 2})
		},
	}
	// The window is from the completion's write to its commit, which spans
	// the whole advance after it. Started together, the release is done
	// before the completion writes anything, so each round starts it a little
	// later than the one before, across that window.
	const rounds = 24
	for name, racer := range racers {
		reopened := 0
		for round := range rounds {
			instanceID, err := svc.StartProcess(ctx, projectID, "open-review", nil)
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			taskID := openTaskOf(ctx, t, svc, projectID, instanceID)
			if err := svc.ClaimTask(ctx, taskID, "ada"); err != nil {
				t.Fatalf("claim: %v", err)
			}

			// Warm two connections, so neither side wins by connecting first.
			var warming sync.WaitGroup
			for range 2 {
				warming.Go(func() {
					if _, err := svc.GetTask(ctx, taskID); err != nil {
						t.Errorf("warm the pool: %v", err)
					}
				})
			}
			warming.Wait()

			start := make(chan struct{})
			var wg sync.WaitGroup
			wg.Go(func() {
				<-start
				_ = svc.CompleteTask(ctx, taskID, "ada", nil)
			})
			wg.Go(func() {
				<-start
				time.Sleep(time.Duration(round) * 250 * time.Microsecond)
				_ = racer(ctx, taskID)
			})
			close(start)
			wg.Wait()

			instance, err := svc.GetInstance(ctx, instanceID)
			if err != nil {
				t.Fatalf("read the instance: %v", err)
			}
			task, err := svc.GetTask(ctx, taskID)
			if err != nil {
				t.Fatalf("read the task: %v", err)
			}
			if instance.Status == entities.ProcessCompleted && task.Status != entities.TaskCompleted {
				reopened++
			}
		}
		if reopened > 0 {
			t.Errorf("%s: in %d of %d races with a completion, the process finished and its task was %s",
				name, reopened, rounds, "open again")
		}
	}
}

func openTaskOf(ctx context.Context, t *testing.T, svc services.ServiceFacade, projectID, instanceID uuid.UUID) uuid.UUID {
	t.Helper()
	tasks, err := svc.ListTasks(ctx, projectID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && task.Status == entities.TaskUnclaimed {
			return task.ID
		}
	}
	t.Fatalf("no open task for instance %s", instanceID)
	return uuid.Nil
}
