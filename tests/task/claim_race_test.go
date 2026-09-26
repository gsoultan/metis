package task_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// A claim read the task, saw it unclaimed, and wrote it claimed, with nothing
// held in between. Two people claiming the same task at the same moment both
// saw it unclaimed and were both told it was theirs; the one whose write
// landed last had it, and the other went to work on a task that was not
// theirs.
func TestOnlyOneOfSeveralSimultaneousClaimsWins(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), observersimpl.NewSSEObserver(),
		"claim-race-test", nil, nil, nil)
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
	if _, err := svc.StartProcess(ctx, projectID, "open-review", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	tasks, err := svc.ListTasks(ctx, projectID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("expected one task: %d, %v", len(tasks), err)
	}
	taskID := tasks[0].ID

	const claimants = 8

	// The pool opens connections lazily. Left cold, the one claim that found a
	// warm connection finished before the others had connected, which is a
	// race the test lost rather than one the code won. A server under load has
	// its connections open.
	warm := make(chan struct{})
	var warming sync.WaitGroup
	for range claimants {
		warming.Go(func() {
			<-warm
			if _, err := svc.GetTask(ctx, taskID); err != nil {
				t.Errorf("warm the pool: %v", err)
			}
		})
	}
	close(warm)
	warming.Wait()

	start := make(chan struct{})
	won := make(chan string, claimants)
	var wg sync.WaitGroup
	for i := range claimants {
		wg.Go(func() {
			<-start
			who := fmt.Sprintf("worker-%d", i)
			if err := svc.ClaimTask(ctx, taskID, who); err == nil {
				won <- who
			}
		})
	}
	close(start)
	wg.Wait()
	close(won)

	var winners []string
	for who := range won {
		winners = append(winners, who)
	}
	if len(winners) != 1 {
		t.Fatalf("%d of %d simultaneous claims were told the task was theirs: %v", len(winners), claimants, winners)
	}
	task, err := svc.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("read the task: %v", err)
	}
	if task.AssigneeUsername() != winners[0] {
		t.Fatalf("the task is held by %q, but %q was told it was theirs", task.AssigneeUsername(), winners[0])
	}
}
