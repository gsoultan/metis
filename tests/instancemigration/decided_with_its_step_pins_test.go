package instancemigration

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// A decision that names a step and the deadline on it.
//
// An approval with a deadline is the commonest shape a removed step has, and
// the new version drops the deadline with it. A migration that decides the
// approval is refused for the deadline's timer, which has nowhere to land,
// unless the deadline is named in a decision too. Nothing is ever taken on the
// deadline itself — no instance waits at a boundary event — but what waits on
// it ends with its step: a skip of the step leaves the timer to be dismissed
// when it comes due, a cancel ends the instance, a hold leaves it where it is.
//
// The planner is about to refuse a decision on a boundary event that could
// never be taken. This is the case that must go on working as it does, written
// and passing before that change was made.

// approvalWithDeadline is approve → sign, with a three-day deadline on the
// approval that leads to an escalation, and sign alone when withApproval is
// false: the approval gone, and its deadline and escalation with it.
func approvalWithDeadline(projectID uuid.UUID, withApproval bool) *entities.ProcessDefinition {
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "approval-with-deadline",
		Name:    "Approval with a deadline",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"p1"}},
			{ID: "sign", Name: "Sign", Type: entities.UserTask, Assignee: "sasha", Incoming: []string{"p2"}, Outgoing: []string{"p3"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"p3"}},
		},
		Flows: []*entities.SequenceFlow{{ID: "p3", SourceRef: "sign", TargetRef: "end"}},
	}
	if !withApproval {
		def.Nodes[1].Incoming = []string{"p1"}
		def.Flows = append(def.Flows, &entities.SequenceFlow{ID: "p1", SourceRef: "start", TargetRef: "sign"})
		return def
	}
	def.Nodes = append(def.Nodes,
		&entities.Node{ID: "approve", Name: "Approve", Type: entities.UserTask, Assignee: "ada", Incoming: []string{"p1"}, Outgoing: []string{"p2"}},
		&entities.Node{
			ID: "deadline", Name: "Three days passed", Type: entities.BoundaryEvent, AttachedToRef: "approve",
			Properties: map[string]any{"event_type": "timer", "timer_duration": "P3D"}, Outgoing: []string{"p4"},
		},
		&entities.Node{ID: "escalate", Name: "Escalate", Type: entities.UserTask, Assignee: "bo", Incoming: []string{"p4"}, Outgoing: []string{"p5"}},
		&entities.Node{ID: "escalated", Type: entities.EndEvent, Incoming: []string{"p5"}})
	def.Flows = append(def.Flows,
		&entities.SequenceFlow{ID: "p1", SourceRef: "start", TargetRef: "approve"},
		&entities.SequenceFlow{ID: "p2", SourceRef: "approve", TargetRef: "sign"},
		&entities.SequenceFlow{ID: "p4", SourceRef: "deadline", TargetRef: "escalate"},
		&entities.SequenceFlow{ID: "p5", SourceRef: "escalate", TargetRef: "escalated"})
	return def
}

// The same decision on the approval and on its deadline: planned, and applied,
// as it always was.
func TestPinADecisionNamingAStepAndTheDeadlineOnIt(t *testing.T) {
	want := map[servicecontracts.NodeActionKind]string{
		servicecontracts.NodeActionSkip:   pinSkipWithItsDeadline,
		servicecontracts.NodeActionCancel: pinCancelWithItsDeadline,
		servicecontracts.NodeActionHold:   pinHoldWithItsDeadline,
	}
	for _, kind := range everyNodeAction {
		t.Run(string(kind), func(t *testing.T) {
			f := newFixture(t)
			v1, v2 := f.startedOn(t, approvalWithDeadline(f.project, true), approvalWithDeadline(f.project, false))
			both := servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
				"approve":  {Kind: kind, Reason: "the approval was dropped"},
				"deadline": {Kind: kind, Reason: "the approval was dropped"},
			})
			// A skip waits for a second administrator; a cancel and a hold do not.
			planned := f.planAndApplyOnOneCall
			if kind == servicecontracts.NodeActionSkip {
				planned = f.planAndApply
			}
			told := planned(t, v1, v2, nil, both, servicecontracts.WithActor("dita"))
			assertToldAs(t, told+ledgerKinds(t, f), want[kind])
			if kind != servicecontracts.NodeActionSkip {
				return
			}

			// Skipped and moved, the deadline's timer is left behind. When the
			// three days pass it is dismissed, and the signature is still to give.
			instance := f.onlyInstance(t)
			if err := f.db.WithContext(f.ctx).Exec(
				`UPDATE jobs SET next_run_at = now() - interval '1 minute' WHERE instance_id = ?`, instance.ID).Error; err != nil {
				t.Fatalf("bring the deadline due: %v", err)
			}
			if err := f.svc.ProcessPendingJobs(f.ctx); err != nil {
				t.Fatalf("process pending jobs: %v", err)
			}
			f.assertWaitingAt(t, v2, "sign")
			if rows := f.jobRows(t, instance.ID); len(rows) != 1 || !strings.HasSuffix(rows[0], "status=completed") {
				t.Errorf("the deadline left behind should have been dismissed when it came due: %v", rows)
			}
			f.runsToItsEnd(t)
		})
	}
}

// ledgerKinds is what the only instance's ledger records, one line a row.
func ledgerKinds(t *testing.T, f *fixture) string {
	t.Helper()
	var out strings.Builder
	for _, row := range f.ledger(t, f.onlyInstance(t).ID) {
		out.WriteString("  row: kind=" + string(row.Kind) + " node=" + nodeOf(row.Node) + "\n")
	}
	return out.String()
}

const pinSkipWithItsDeadline = `
plan: instances=1 refusals=[] warnings=[]
  move: approve→approve tokens=1 tasks=1 jobs=0 events=0 mapped=false
  move: deadline→deadline tokens=0 tasks=0 jobs=1 events=0 mapped=false
error: <nil>
result: changed=1 passed_over=0
instance: status=active on=v2 tokens=[sign] completed=[start approve]
  open task: node=sign name="Sign"
  job: definition=v2 node=deadline type=timer status=pending
  row: kind=waive node=approve/Approve
`

const pinCancelWithItsDeadline = `
plan: instances=1 refusals=[] warnings=[]
  move: approve→approve tokens=1 tasks=1 jobs=0 events=0 mapped=false
  move: deadline→deadline tokens=0 tasks=0 jobs=1 events=0 mapped=false
error: <nil>
result: changed=1 passed_over=0
instance: status=cancelled on=v1 tokens=[] completed=[start]
  job: definition=v1 node=deadline type=timer status=pending
  row: kind=cancel node=approve/Approve
`

const pinHoldWithItsDeadline = `
plan: instances=1 refusals=[] warnings=[]
  move: approve→approve tokens=1 tasks=1 jobs=0 events=0 mapped=false
  move: deadline→deadline tokens=0 tasks=0 jobs=1 events=0 mapped=false
error: <nil>
result: changed=1 passed_over=0
instance: status=active on=v1 tokens=[approve] completed=[start]
  open task: node=approve name="Approve"
  job: definition=v1 node=deadline type=timer status=pending
  row: kind=hold node=approve/Approve
`
