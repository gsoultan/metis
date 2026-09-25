package bpmn_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// attestors is more people than one read of the store returns: storm starts
// every query with a limit of 1000.
const attestors = 1050

// A deadline that cuts an activity short withdraws every task it had open.
//
// The interrupt found the activity's open tasks by reading the instance's tasks
// through a query the store caps at a thousand rows, newest first. On an
// activity that asks more than a thousand people at once — everyone
// acknowledging a new policy — the oldest tasks were never withdrawn. They
// stayed in those people's inboxes for work the process had already abandoned.
func TestADeadlineWithdrawsEveryOpenTaskOfTheActivityItInterrupts(t *testing.T) {
	h := newEngineHarness(t, "Policy Attestation Project")
	ctx := h.Ctx()
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "policy-attestation",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "attest", Type: entities.UserTask, Name: "Acknowledge the policy",
				MultiInstanceType: "parallel", Collection: "people", ElementVariable: "person"},
			{ID: "deadline", Type: entities.BoundaryEvent, AttachedToRef: "attest", Properties: map[string]any{
				"timer_duration": "P7D",
			}},
			{ID: "chase", Type: entities.UserTask, Name: "Chase whoever has not answered"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "attest"},
			{ID: "f2", SourceRef: "attest", TargetRef: "end"},
			{ID: "f3", SourceRef: "deadline", TargetRef: "chase"},
		},
	})

	people := make([]any, attestors)
	for i := range people {
		people[i] = fmt.Sprintf("employee-%04d", i)
	}
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "policy-attestation", map[string]any{"people": people})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if open := h.openTasksOn(t, instanceID, "attest"); open != attestors {
		t.Fatalf("the activity asked %d people; want %d", open, attestors)
	}

	// The week passes.
	if moved := h.dueNow(ctx, t, instanceID); moved == 0 {
		t.Fatal("no deadline was waiting to come due")
	}
	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	if !h.waitingAt(ctx, t, instanceID, "chase") {
		t.Fatal("the deadline did not take the process on to chasing")
	}
	if open := h.openTasksOn(t, instanceID, "attest"); open > 0 {
		t.Fatalf("the deadline ended the activity and %d of its %d tasks are still open in somebody's inbox",
			open, attestors)
	}
}

// openTasksOn counts an instance's open tasks on one node in the database, so
// the count depends on neither the read under test nor a paged read, whose
// order ties between tasks created in one transaction.
func (h engineHarness) openTasksOn(t *testing.T, instanceID uuid.UUID, nodeID string) int {
	t.Helper()
	var open int64
	if err := h.db.Raw(`SELECT count(*) FROM tasks
		 WHERE instance_id = ? AND node_id = ? AND deleted_at IS NULL AND status IN (?, ?, ?)`,
		instanceID, nodeID, entities.TaskUnclaimed, entities.TaskClaimed, entities.TaskDelegated).
		Scan(&open).Error; err != nil {
		t.Fatalf("count open tasks: %v", err)
	}
	return int(open)
}
