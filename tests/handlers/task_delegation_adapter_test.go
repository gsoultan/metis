package handlers_test

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// Every write to a task goes entity → model → row and back. A field the
// adapters do not carry is one the next hand-over silently erases.
func TestTaskAdapters_CarryWhoATaskGoesBackTo(t *testing.T) {
	entity := adapters.TaskEntityAdapter{Model: models.TaskModel{
		NodeID: "approve", IterationID: "2", Status: models.TaskDelegated, Assignee: "citra",
		Owner: "budi", DelegationState: "pending",
	}}.ToEntity()
	if entity.OwnerUsername() != "budi" || entity.DelegationState != entities.DelegationPending || !entity.AwaitsHandBack() {
		t.Fatalf("read as owner %q, state %q, awaiting hand-back %v", entity.OwnerUsername(), entity.DelegationState, entity.AwaitsHandBack())
	}
	back := adapters.TaskModelAdapter{Task: entity}.ToModel()
	if back.Owner != "budi" || back.DelegationState != "pending" {
		t.Fatalf("written back as owner %q, state %q", back.Owner, back.DelegationState)
	}
	// The pin: which run of a repeating approval the task is for (slice 1) is
	// carried through the same two literals, and still is.
	if entity.IterationID != "2" || back.IterationID != "2" {
		t.Fatalf("the task's iteration was read as %q and written back as %q, want 2 both ways", entity.IterationID, back.IterationID)
	}
}

func TestTaskAdapters_ATaskNeverDelegatedHasNoOwner(t *testing.T) {
	entity := adapters.TaskEntityAdapter{Model: models.TaskModel{NodeID: "approve", Status: models.TaskClaimed, Assignee: "budi"}}.ToEntity()
	if entity.Owner != nil || entity.DelegationState != "" || entity.AwaitsHandBack() {
		t.Fatalf("a claimed task reads as owned by %v in state %q", entity.Owner, entity.DelegationState)
	}
	// A row an old release wrote: delegated, and nobody to go back to. It is
	// not waiting for a hand-back nobody can make.
	stranded := adapters.TaskEntityAdapter{Model: models.TaskModel{NodeID: "approve", Status: models.TaskDelegated, Assignee: "citra"}}.ToEntity()
	if stranded.AwaitsHandBack() {
		t.Fatal("a delegation with no owner is waiting to be handed back to nobody")
	}
}
