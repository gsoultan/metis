package bpmn_test

import (
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/transports/adapters"
)

// The inbox's urgency display is computed from two fields the engine has to put
// on the task: how important it is, and when it is due.
//
// Both are authored on the BPMN node, so they have to survive being copied onto
// the task the engine creates and then out through the wire adapter. Nothing
// asserted that end to end, and the failure would be silent: the inbox would
// simply show every task as ordinary and never overdue.
func TestUserTaskCarriesPriorityAndDueDate(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)

	due := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "expense-approval",
		Name:    "With an SLA",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{
				ID:       "hold",
				Type:     entities.UserTask,
				Name:     "Approve",
				Assignee: "approver",
				Priority: 75,
				DueDate:  due.Format(time.RFC3339),
			},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "hold"},
			{ID: "f2", SourceRef: "hold", TargetRef: "end"},
		},
	}
	if _, err := svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if _, err := svc.StartProcess(ctx, projectID, "expense-approval", nil); err != nil {
		t.Fatalf("start: %v", err)
	}

	tasks, err := svc.ListTasks(ctx, projectID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one task, got %d", len(tasks))
	}
	task := tasks[0]

	if task.Priority != 75 {
		t.Errorf("the node's priority should reach the task, got %d", task.Priority)
	}
	if task.DueDate == nil {
		t.Fatal("the node's due date should reach the task, got none")
	}
	if !task.DueDate.UTC().Equal(due) {
		t.Errorf("due date should round-trip, wanted %s got %s", due, task.DueDate.UTC())
	}

	// And out through the wire adapter, which is what the inbox actually reads.
	wire := adapters.TaskPBAdapter{Task: task}.ToProto()
	if wire.Priority != 75 {
		t.Errorf("priority should survive the wire adapter, got %d", wire.Priority)
	}
	if wire.DueDate == "" {
		t.Fatal("due date should survive the wire adapter, got empty")
	}
	parsed, err := time.Parse(time.RFC3339, wire.DueDate)
	if err != nil {
		t.Fatalf("the wire due date should be RFC 3339, got %q: %v", wire.DueDate, err)
	}
	if !parsed.UTC().Equal(due) {
		t.Errorf("wire due date should round-trip, wanted %s got %s", due, parsed.UTC())
	}
}
