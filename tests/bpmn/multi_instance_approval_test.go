package bpmn_test

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/tests/testutils"
)

// An approval several people give: one task each, at once or in turn.
//
// These tests go through the task service — the inbox's "Complete" — because
// that is how an approval is given. Every earlier multi-instance test finished
// its iterations through the job worker or by calling the engine directly, and
// both of those name the iteration; the task service did not.

// approvalDefinition is start → approve (once per approver) → record → end.
// The approval's form declares "decision", so a completion may set it.
func approvalDefinition(projID uuid.UUID, key, loop, completionCondition string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projID},
		Key:     key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{
				ID:                  "approve",
				Type:                entities.UserTask,
				Name:                "Approve the purchase",
				MultiInstanceType:   loop,
				Collection:          "approvers",
				ElementVariable:     "approver",
				CompletionCondition: completionCondition,
				Properties:          testutils.FormDeclaring("decision"),
			},
			{ID: "record", Type: entities.UserTask, Name: "Record the outcome"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "record"},
			{ID: "f3", SourceRef: "record", TargetRef: "end"},
		},
	}
}

// startApproval deploys def and starts one instance asking the approvers.
func startApproval(t *testing.T, h engineHarness, def *entities.ProcessDefinition, approvers ...any) uuid.UUID {
	t.Helper()
	h.deploy(t, def)
	instanceID, err := h.svc.StartProcess(h.Ctx(), h.projID, def.Key, map[string]any{"approvers": approvers})
	if err != nil {
		t.Fatalf("start %s: %v", def.Key, err)
	}
	return instanceID
}

// openIterationTasks returns the instance's open tasks on nodeID, ordered by
// the iteration each was created for and then by id, so a test can name "the
// first" without depending on the order a listing happens to return.
func openIterationTasks(ctx context.Context, t *testing.T, h engineHarness, instanceID uuid.UUID, nodeID string) []entities.Task {
	t.Helper()
	tasks, err := h.svc.ListTasks(ctx, h.projID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	var open []entities.Task
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == nodeID && taskIsOpen(task.Status) {
			open = append(open, task)
		}
	}
	slices.SortFunc(open, func(a, b entities.Task) int {
		return cmp.Or(
			cmp.Compare(iterationIndex(a), iterationIndex(b)),
			cmp.Compare(a.ID.String(), b.ID.String()),
		)
	})
	return open
}

// iterationIndex is the task's iteration as a number, or -1 when it records
// none.
func iterationIndex(task entities.Task) int {
	index, err := strconv.Atoi(task.IterationID)
	if err != nil {
		return -1
	}
	return index
}

// tokenIterationsOn returns the iteration of every token the instance holds on
// nodeID, sorted.
func tokenIterationsOn(ctx context.Context, t *testing.T, h engineHarness, instanceID uuid.UUID, nodeID string) []string {
	t.Helper()
	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	iterations := []string{}
	for _, token := range instance.GetTokensByNode(&entities.Node{ID: nodeID}) {
		iterations = append(iterations, token.IterationID)
	}
	slices.Sort(iterations)
	return iterations
}

// tasksEverOn counts every task the instance has had on nodeID, whatever its
// status, in the database. A step reached twice shows up here as two.
func tasksEverOn(t *testing.T, h engineHarness, instanceID uuid.UUID, nodeID string) int {
	t.Helper()
	var count int64
	if err := h.db.Raw(`SELECT count(*) FROM tasks
		 WHERE instance_id = ? AND node_id = ? AND deleted_at IS NULL`,
		instanceID, nodeID).Scan(&count).Error; err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	return int(count)
}

// completeAs completes a task as an operator named who. A task nobody was
// named for is an operator's to take, which is what these approvals are.
func completeAs(ctx context.Context, h engineHarness, task entities.Task, who string, vars map[string]any) error {
	return h.svc.CompleteTask(testutils.AsOperator(ctx, who), task.ID, who, vars)
}

// requireInstanceStatus reloads the instance and fails unless it is in want.
func requireInstanceStatus(ctx context.Context, t *testing.T, h engineHarness, instanceID uuid.UUID, want entities.ProcessStatus) entities.ProcessInstance {
	t.Helper()
	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	if instance.Status != want {
		t.Fatalf("the instance is %q, want %q (tokens: %d)", instance.Status, want, len(instance.Tokens))
	}
	return instance
}

// BPMN 2.0.2 §13.2.7 (Multiple Instances Activity): each instance of the
// activity is its own, with its own token. The task created for one has to say
// which, or nothing that completes it can.
//
// A task row stored no iteration, so every completion named none.
func TestEachTaskOfAMultiInstanceStepRecordsItsIteration(t *testing.T) {
	h := newEngineHarness(t, "Iteration Recording Project")
	ctx := h.Ctx()
	instanceID := startApproval(t, h, approvalDefinition(h.projID, "purchase-approval", "parallel", ""),
		"ana", "budi", "citra")

	open := openIterationTasks(ctx, t, h, instanceID, "approve")
	if len(open) != 3 {
		t.Fatalf("three approvers were asked and %d task(s) are open", len(open))
	}
	recorded := make([]string, 0, len(open))
	for _, task := range open {
		recorded = append(recorded, task.IterationID)
	}
	if !slices.Equal(recorded, []string{"0", "1", "2"}) {
		t.Fatalf("the three approvals record iterations %v, want [0 1 2]", recorded)
	}

	// Claiming writes the whole row back. An approver who claims before
	// completing must not lose the iteration on the way.
	if err := h.svc.ClaimTask(testutils.AsOperator(ctx, "budi"), open[1].ID, "budi"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	claimed, err := h.svc.GetTask(ctx, open[1].ID)
	if err != nil {
		t.Fatalf("re-read the claimed task: %v", err)
	}
	if claimed.IterationID != "1" {
		t.Fatalf("after a claim the task records iteration %q, want 1", claimed.IterationID)
	}
}
