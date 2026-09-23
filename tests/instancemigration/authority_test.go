package instancemigration

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// Delegation of authority.
//
// "Under 50k the team lead, over 50k the CFO" is a value-banded approval
// matrix, and it is business policy: it changes far more often than the shape
// of the process does. Written into the diagram it takes a modeller, a redeploy
// and a regression test to move a threshold.
//
// So it is not in the diagram. A user task names a decision table and takes its
// assignee from whatever that table decides — the process says *that* somebody
// approves, the table says who. This proves the whole path end to end, because
// the mechanism had unit tests and nothing that ran an actual instance through
// it.
//
// The band is read once, when the task is created, and that is the last moment
// before the work exists: there is no way to amend an instance variable behind
// a task that is already open, so the approver a matrix chose cannot go stale
// under it. Should one ever be added, this is the test that has to grow a
// second half.

// authorityMatrix is the delegation-of-authority table: the band decides the
// approver.
func authorityMatrix(projectID uuid.UUID) entities.DecisionDefinition {
	return entities.DecisionDefinition{
		ID:      uuid.Must(uuid.NewV7()),
		Project: &entities.Project{ID: projectID},
		Key:     "approvalAuthority",
		Name:    "Approval authority",
		Inputs: []entities.DecisionInput{
			{ID: "in1", Label: "Amount", Expression: "amount", Type: "number"},
		},
		Outputs: []entities.DecisionOutput{
			{ID: "out1", Label: "Assignee", Name: "assignee", Type: "string"},
		},
		Rules: []entities.DecisionRule{
			{ID: "r1", Inputs: []string{"> 50000"}, Outputs: []any{"cfo"}},
			{ID: "r2", Inputs: []string{"<= 50000"}, Outputs: []any{"team-lead"}},
		},
	}
}

// bandedApproval is start → approve → end, where "approve" asks the matrix who
// should do it.
func bandedApproval(projectID uuid.UUID) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "banded-approval",
		Name:    "Banded approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"b1"}},
			{
				ID: "approve", Name: "Approve", Type: entities.UserTask,
				// No assignee in the diagram: the table decides.
				Properties: map[string]any{"assignment_decision_key": "approvalAuthority"},
				Incoming:   []string{"b1"}, Outgoing: []string{"b2"},
			},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"b2"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "b1", SourceRef: "start", TargetRef: "approve"},
			{ID: "b2", SourceRef: "approve", TargetRef: "end"},
		},
	}
}

// TestTheAuthorityMatrixDecidesWhoApproves.
func TestTheAuthorityMatrixDecidesWhoApproves(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.CreateDecision(f.ctx, authorityMatrix(f.project)); err != nil {
		t.Fatalf("deploy the authority matrix: %v", err)
	}
	if _, err := f.svc.CreateDefinition(f.ctx, bandedApproval(f.project)); err != nil {
		t.Fatalf("deploy the process: %v", err)
	}

	for _, band := range []struct {
		name     string
		amount   float64
		approver string
	}{
		{"under the threshold", 9_000, "team-lead"},
		{"over the threshold", 60_000, "cfo"},
	} {
		t.Run(band.name, func(t *testing.T) {
			instanceID, err := f.svc.StartProcess(f.ctx, f.project, "banded-approval",
				map[string]any{"amount": band.amount})
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			tasks, err := f.svc.ListTasks(f.ctx, f.project)
			if err != nil {
				t.Fatalf("list tasks: %v", err)
			}
			for _, task := range tasks {
				if task.Instance == nil || task.Instance.ID != instanceID {
					continue
				}
				if task.Assignee == nil {
					t.Fatalf("a %v approval landed on nobody; the matrix should have chosen %q",
						band.amount, band.approver)
				}
				if task.Assignee.Username != band.approver {
					t.Fatalf("a %v approval landed on %q; the matrix says %q",
						band.amount, task.Assignee.Username, band.approver)
				}
				return
			}
			t.Fatal("no task was created for this instance")
		})
	}
}
