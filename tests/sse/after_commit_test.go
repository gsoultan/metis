package sse_test

import (
	"strings"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// A live-update hint tells a browser to refetch. It was sent from inside the
// transaction that produced it: a browser that refetched at once could read
// the state from before, and got no second hint, and a hint about work that
// then rolled back pointed at something that never happened.
//
// Completing a task whose next step fails is that case: the completion rolls
// back, and the inbox was still told the task was completed.
func TestAHintIsSentOnlyForWorkThatCommitted(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	dispatcher := observersimpl.NewEventDispatcher()
	sse := observersimpl.NewSSEObserver()
	// Wired as internal/app wires it.
	sse.DeliverAfterCommitWith(repo.UnitOfWork().AfterCommit)
	dispatcher.Register(sse)
	svc := services.NewServiceFacade(repo, dispatcher, sse, "sse-after-commit", nil, nil, nil)
	ctx, orgID, projectID := testutils.ScopedProject(t, repo)

	if _, err := svc.CreateDefinition(ctx, &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "claim",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the claim", Assignee: "rita"},
			{ID: "decide", Type: entities.ExclusiveGateway},
			{ID: "accepted", Type: entities.EndEvent},
			{ID: "rejected", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "in", SourceRef: "start", TargetRef: "review"},
			{ID: "out", SourceRef: "review", TargetRef: "decide"},
			{ID: "yes", SourceRef: "decide", TargetRef: "accepted", Condition: "approved"},
			{ID: "no", SourceRef: "decide", TargetRef: "rejected", Condition: "rejected"},
		},
	}); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if _, err := svc.StartProcess(ctx, projectID, "claim", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	tasks, err := svc.ListTasks(ctx, projectID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("expected the review task: %d, %v", len(tasks), err)
	}

	browser := sse.AddClient(entities.SSEScope{Organization: orgID})
	t.Cleanup(func() { sse.RemoveClient(browser) })

	// No answer to the question the gateway asks: the advance fails and the
	// completion rolls back.
	if err := svc.CompleteTask(ctx, tasks[0].ID, "rita", nil); err == nil {
		t.Fatal("the completion went through, though nothing after the review could be chosen")
	}
	if hint, told := taskCompletedHint(browser, 200*time.Millisecond); told {
		t.Fatalf("the inbox was told a task was completed by a completion that rolled back: %s", hint)
	}

	// With an answer it commits, and the hint follows.
	if err := svc.CompleteTask(ctx, tasks[0].ID, "rita", map[string]any{"approved": true}); err != nil {
		t.Fatalf("complete the review: %v", err)
	}
	if _, told := taskCompletedHint(browser, 2*time.Second); !told {
		t.Fatal("a completion that committed sent no hint")
	}
}

// taskCompletedHint waits up to wait for a TaskCompleted hint on the channel.
func taskCompletedHint(browser chan string, wait time.Duration) (string, bool) {
	deadline := time.After(wait)
	for {
		select {
		case hint := <-browser:
			if strings.Contains(hint, entities.EventTaskCompleted) {
				return hint, true
			}
		case <-deadline:
			return "", false
		}
	}
}
