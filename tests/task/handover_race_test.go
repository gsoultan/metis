package task_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// Review focus 2. Whether the caller holds the task was decided from a read
// nothing held, and the write came afterwards. Several hand-overs by the same
// holder at the same moment were all judged the holder's, and the task ended
// with whoever wrote last while the others had been told it went to theirs.
// Decided under the row lock, the first one moves the task and the rest are
// no longer its holder's.
func TestOnlyOneOfSeveralSimultaneousHandOversByTheHolderWins(t *testing.T) {
	repo, svc, ctx, projectID := newTaskService(t)
	const receivers = 8
	for i := range receivers {
		seedMember(t, repo, ctx, fmt.Sprintf("worker-%d", i))
	}
	seedMember(t, repo, ctx, "alice")
	taskID := seedTask(t, repo, ctx, projectID, entities.Task{
		Name:     "Approve the refund",
		Type:     entities.UserTask,
		Status:   entities.TaskClaimed,
		Assignee: &entities.User{Username: "alice"},
		Node:     &entities.Node{ID: "approve"},
		Instance: seedCase(t, repo, ctx, projectID),
	})

	// A server under load has its connections open; left cold, the one
	// hand-over that found a warm connection finishes before the others have
	// connected, which is a race the test lost rather than one the code won.
	var warming sync.WaitGroup
	for range receivers {
		warming.Go(func() {
			if _, err := svc.GetTask(ctx, taskID); err != nil {
				t.Errorf("warm the pool: %v", err)
			}
		})
	}
	warming.Wait()

	start := make(chan struct{})
	won := make(chan string, receivers)
	var wg sync.WaitGroup
	for i := range receivers {
		wg.Go(func() {
			<-start
			to := fmt.Sprintf("worker-%d", i)
			if err := svc.AssignTask(ctx, taskID, servicecontracts.HandOver{Actor: "alice", Target: to}); err == nil {
				won <- to
			}
		})
	}
	close(start)
	wg.Wait()
	close(won)

	var winners []string
	for to := range won {
		winners = append(winners, to)
	}
	if len(winners) != 1 {
		t.Fatalf("%d of %d simultaneous hand-overs by the holder were made: %v; once the first moved the task alice no longer held it",
			len(winners), receivers, winners)
	}
	task, err := svc.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("read the task: %v", err)
	}
	if task.AssigneeUsername() != winners[0] {
		t.Fatalf("the task is held by %q, but the hand-over that was made was to %q", task.AssigneeUsername(), winners[0])
	}
}
