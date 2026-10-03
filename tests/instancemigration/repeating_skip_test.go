package instancemigration

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/tests/testutils"
)

// repeatingApproval is start → approve (one run per approver) → record → end,
// and, without the approval, start → record → end: the version that drops it.
//
// The steps name their flows, as the designer saves them and as every fixture
// in this package does: the migration's planner counts a step's ways out from
// the step itself, so a skip of a step that names none is refused for having
// nowhere to advance to.
func repeatingApproval(projectID uuid.UUID, loop string, withApproval bool) *entities.ProcessDefinition {
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "repeating-approval-" + loop,
		Name:    "Repeating approval",
	}
	start := &entities.Node{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}}
	record := &entities.Node{ID: "record", Type: entities.UserTask, Name: "Record the outcome",
		Incoming: []string{"f2"}, Outgoing: []string{"f3"}}
	end := &entities.Node{ID: "end", Type: entities.EndEvent, Incoming: []string{"f3"}}
	last := &entities.SequenceFlow{ID: "f3", SourceRef: "record", TargetRef: "end"}
	if !withApproval {
		record.Incoming = []string{"f1"}
		def.Nodes = []*entities.Node{start, record, end}
		def.Flows = []*entities.SequenceFlow{{ID: "f1", SourceRef: "start", TargetRef: "record"}, last}
		return def
	}
	approve := &entities.Node{ID: "approve", Type: entities.UserTask, Name: "Approve the purchase", MultiInstanceType: loop,
		Collection: "approvers", ElementVariable: "approver", Properties: testutils.FormDeclaring("decision"),
		Incoming: []string{"f1"}, Outgoing: []string{"f2"}}
	def.Nodes = []*entities.Node{start, approve, record, end}
	def.Flows = []*entities.SequenceFlow{
		{ID: "f1", SourceRef: "start", TargetRef: "approve"},
		{ID: "f2", SourceRef: "approve", TargetRef: "record"},
		last,
	}
	return def
}

func (f *fixture) tasksOn(t *testing.T, nodeID string) (open, ever int) {
	t.Helper()
	all, err := f.svc.ListTasks(f.ctx, f.project)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range all {
		if task.NodeID() != nodeID {
			continue
		}
		ever++
		if task.Status == entities.TaskUnclaimed || task.Status == entities.TaskClaimed || task.Status == entities.TaskDelegated {
			open++
		}
	}
	return open, ever
}

// BPMN 2.0.2 §13.2.7 / §10.3.8: a multi-instance activity that is finished
// leaves no instance of itself behind. A migration skip advanced past a
// repeating approval with Proceed and no run named: one run was counted, the
// other runs' tokens stayed with every task withdrawn — and a sequential
// approval started its next run. The rewrite then found a token still on a
// step the migration decides and passed the instance over, so it took one run
// of the same migration for each remaining run of the approval to get it off
// the step, and between runs it waited there with tokens and no task.
//
// Both targets: the new version keeps the approval, and — the case a skip
// exists for — the new version drops it.
func TestASkippedRepeatingApprovalLeavesNoRunBehind(t *testing.T) {
	targets := []struct {
		name         string
		keepsTheStep bool
	}{
		{"the new version keeps the step", true},
		{"the new version drops the step", false},
	}
	for _, loop := range []string{"parallel", "sequential"} {
		for _, target := range targets {
			t.Run(loop+"/"+target.name, func(t *testing.T) {
				f := newFixture(t)
				v1, err := f.svc.CreateDefinition(f.ctx, repeatingApproval(f.project, loop, true))
				if err != nil {
					t.Fatalf("deploy v1: %v", err)
				}
				if _, err := f.svc.StartProcess(f.ctx, f.project, "repeating-approval-"+loop,
					map[string]any{"approvers": []any{"ana", "budi", "citra"}}); err != nil {
					t.Fatalf("start: %v", err)
				}
				if loop == "parallel" {
					first := f.openTasks(t)[0]
					if err := f.svc.CompleteTask(testutils.AsOperator(f.ctx, "carol"), first.ID, "carol", map[string]any{"decision": "yes"}); err != nil {
						t.Fatalf("complete one approval: %v", err)
					}
				}
				_, approvalsBefore := f.tasksOn(t, "approve")
				v2, err := f.svc.CreateDefinition(f.ctx, repeatingApproval(f.project, loop, target.keepsTheStep))
				if err != nil {
					t.Fatalf("deploy v2: %v", err)
				}

				if err := f.svc.MigrateInstances(f.ctx, v1, v2, nil,
					servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
						"approve": {Kind: servicecontracts.NodeActionSkip, Reason: "the purchase was approved by the board instead"},
					}),
					servicecontracts.WithActor("dita")); err != nil {
					t.Fatalf("apply: %v", err)
				}

				instance := f.onlyInstance(t)
				for _, token := range instance.Tokens {
					if token.Node != nil && token.Node.ID == "approve" {
						t.Fatalf("the skipped approval still holds a token for run %q", token.IterationID)
					}
				}
				if instance.Definition == nil || instance.Definition.ID != v2 {
					t.Fatal("the instance was passed over: one run of the migration must skip the whole step and move it")
				}
				if open, ever := f.tasksOn(t, "approve"); open != 0 || ever != approvalsBefore {
					t.Fatalf("after the skip the approval has %d open task(s) and %d in all, want none open and %d in all (no new run)",
						open, ever, approvalsBefore)
				}
				if open, _ := f.tasksOn(t, "record"); open != 1 {
					t.Fatalf("the step after the approval has %d open task(s), want exactly one", open)
				}
				record := f.openTasks(t)[0]
				if err := f.svc.CompleteTask(testutils.AsOperator(f.ctx, "carol"), record.ID, "carol", nil); err != nil {
					t.Fatalf("complete the recording: %v", err)
				}
				if got := f.onlyInstance(t); got.Status != entities.ProcessCompleted || len(got.Tokens) != 0 {
					t.Fatalf("the instance is %s with %d token(s), want completed and empty", got.Status, len(got.Tokens))
				}
			})
		}
	}
}
