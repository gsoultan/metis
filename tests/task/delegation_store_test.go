package task_test

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// The store writes every column of a task back on each update. Until it knows
// the two new ones, the first claim, edit or completion of a delegated task
// writes them back as NULL.
func TestWhoATaskGoesBackToSurvivesTheStore(t *testing.T) {
	repo, svc, ctx, projectID := newTaskService(t)
	taskID := seedTask(t, repo, ctx, projectID, entities.Task{
		Name: "Approve the refund", Type: entities.UserTask, Status: entities.TaskDelegated,
		Assignee:        &entities.User{Username: "citra"},
		Owner:           &entities.User{Username: "budi"},
		DelegationState: entities.DelegationPending,
		Node:            &entities.Node{ID: "approve"},
		// One run of a repeating approval: the column slice 1 added through
		// the same store. It has to survive everything below as well.
		IterationID: "2",
	})

	read, err := svc.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("read the task: %v", err)
	}
	if read.OwnerUsername() != "budi" || read.DelegationState != entities.DelegationPending {
		t.Fatalf("stored with owner budi, pending; read back owner %q, state %q", read.OwnerUsername(), read.DelegationState)
	}
	// The pin, asked last (deferred, not t.Cleanup: the test's context is
	// cancelled before cleanups run): after the insert, both updates below and
	// the regenerated store, the task still says which run it is for.
	defer func() {
		if after, err := svc.GetTask(ctx, taskID); err != nil || after.IterationID != "2" {
			t.Errorf("the task's iteration reads %q at the end (%v), want 2: the store dropped slice 1's column", after.IterationID, err)
		}
	}()

	// And through an update that changes something else.
	row, err := repo.Task().Get(ctx, taskID)
	if err != nil {
		t.Fatalf("read the row: %v", err)
	}
	row.Priority = 40
	if err := repo.Task().Update(ctx, row); err != nil {
		t.Fatalf("update the row: %v", err)
	}
	if read, err = svc.GetTask(ctx, taskID); err != nil || read.OwnerUsername() != "budi" || read.DelegationState != entities.DelegationPending {
		t.Fatalf("after an update of its priority the task reads owner %q, state %q (%v)", read.OwnerUsername(), read.DelegationState, err)
	}

	// Cleared means NULL, not the empty string.
	row.Owner, row.DelegationState = "", ""
	if err := repo.Task().Update(ctx, row); err != nil {
		t.Fatalf("clear the delegation: %v", err)
	}
	if read, err = svc.GetTask(ctx, taskID); err != nil || read.Owner != nil || read.DelegationState != "" {
		t.Fatalf("after clearing, the task reads owner %v, state %q (%v)", read.Owner, read.DelegationState, err)
	}
}
