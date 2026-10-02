package task_test

import (
	"context"
	"slices"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	observerimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// seenEvents records each event the task service raises, as its type and who
// it says it is about.
type seenEvents struct{ told []string }

func (s *seenEvents) OnEvent(_ context.Context, event entities.ProcessEvent) {
	about := event.Assignee
	if about == "" {
		about, _ = event.Variables["assignee"].(string)
	}
	s.told = append(s.told, event.Type+" "+about)
}

// Webhooks, the event stream and the notifier learn of a hand-over from the
// event it raises: every registered webhook endpoint is posted every event, by
// type. Recording who made a hand-over must not change which event that is.
func TestObserversStillSeeTheEventsHandOversAlwaysRaised(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	seen := &seenEvents{}
	dispatcher := observerimpl.NewEventDispatcher()
	dispatcher.Register(seen)
	engine := serviceimpl.NewExecutionEngine(repo, dispatcher)
	svc := serviceimpl.NewTaskService(repo, engine, serviceimpl.NewAuditWriter(repo.Audit()))
	ctx, _, projectID := testutils.ScopedProject(t, repo)
	seedMember(t, repo, ctx, "alice")
	seedMember(t, repo, ctx, "bob")
	taskID := seedTask(t, repo, ctx, projectID, entities.Task{
		Name: "Approve the refund", Type: entities.UserTask, Status: entities.TaskClaimed,
		Assignee: &entities.User{Username: "alice"}, Node: &entities.Node{ID: "approve"}, Instance: seedCase(t, repo, ctx, projectID),
	})

	if err := svc.AssignTask(ctx, taskID, servicecontracts.HandOver{Actor: "alice", Target: "bob"}); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := svc.UnclaimTask(ctx, taskID, servicecontracts.HandOver{Actor: "bob"}); err != nil {
		t.Fatalf("release: %v", err)
	}

	want := []string{entities.EventTaskClaimed + " bob", entities.EventTaskUpdated + " "}
	if !slices.Equal(seen.told, want) {
		t.Fatalf("an assignment and a release raised %q; they have always raised %q", seen.told, want)
	}
}
