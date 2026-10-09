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
// The approval is work somebody does, of the kind given: a user task or a
// manual one.
//
// The steps name their flows, as the designer saves them and as every fixture
// in this package does: the migration's planner counts a step's ways out from
// the step itself, so a skip of a step that names none is refused for having
// nowhere to advance to.
func repeatingApproval(projectID uuid.UUID, kind entities.NodeType, loop string, withApproval bool) *entities.ProcessDefinition {
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
	approve := &entities.Node{ID: "approve", Type: kind, Name: "Approve the purchase", MultiInstanceType: loop,
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

// theOpenTask is the one task still in somebody's hands, and fails the test
// when there is any other number of them.
func (f *fixture) theOpenTask(t *testing.T, what string) entities.Task {
	t.Helper()
	open := f.openTasks(t)
	if len(open) != 1 {
		t.Fatalf("%s: %d task(s) are open, want exactly one", what, len(open))
	}
	return open[0]
}

// theWaiverOf is the one ledger row that says nodeID was waived on the
// instance, and fails the test when there is any other number of them.
func (f *fixture) theWaiverOf(t *testing.T, instanceID uuid.UUID, nodeID string) entities.Deviation {
	t.Helper()
	var waivers []entities.Deviation
	for _, row := range f.ledger(t, instanceID) {
		if row.Kind == entities.DeviationWaive && row.Node != nil && row.Node.ID == nodeID {
			waivers = append(waivers, row)
		}
	}
	if len(waivers) != 1 {
		t.Fatalf("the ledger holds %d waiver(s) of %q, want exactly one", len(waivers), nodeID)
	}
	return waivers[0]
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
// exists for — the new version drops it. And both kinds of work somebody
// does: a manual task repeats and is counted exactly as a user task is.
func TestASkippedRepeatingApprovalLeavesNoRunBehind(t *testing.T) {
	kinds := []struct {
		name string
		kind entities.NodeType
	}{
		{"user task", entities.UserTask},
		{"manual task", entities.ManualTask},
	}
	targets := []struct {
		name         string
		keepsTheStep bool
	}{
		{"the new version keeps the step", true},
		{"the new version drops the step", false},
	}
	for _, kind := range kinds {
		for _, loop := range []string{"parallel", "sequential"} {
			for _, target := range targets {
				t.Run(kind.name+"/"+loop+"/"+target.name, func(t *testing.T) {
					skipARepeatingApproval(t, kind.kind, loop, target.keepsTheStep)
				})
			}
		}
	}
}

// skipARepeatingApproval starts an approval of three runs, skips it with one
// migration, and asks that nothing of the step is left: no token, no count of
// its runs, no open task, no further run — and that what was withdrawn was
// announced and recorded, once for each run still open.
func skipARepeatingApproval(t *testing.T, kind entities.NodeType, loop string, keepsTheStep bool) {
	f := newFixture(t)
	watcher := &withdrawalWatcher{}
	f.dispatcher.Register(watcher)
	v1, err := f.svc.CreateDefinition(f.ctx, repeatingApproval(f.project, kind, loop, true))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "repeating-approval-"+loop,
		map[string]any{"approvers": []any{"ana", "budi", "citra"}}); err != nil {
		t.Fatalf("start: %v", err)
	}
	// A parallel approval opens all three runs and one is completed, leaving
	// two; a sequential one has only its first run open.
	stillOpen := 1
	if loop == "parallel" {
		open := f.openTasks(t)
		if len(open) != 3 {
			t.Fatalf("a parallel approval over three approvers opened %d task(s), want three", len(open))
		}
		if err := f.svc.CompleteTask(testutils.AsOperator(f.ctx, "carol"), open[0].ID, "carol", map[string]any{"decision": "yes"}); err != nil {
			t.Fatalf("complete one approval: %v", err)
		}
		stillOpen = 2
	}
	openBefore, approvalsBefore := f.tasksOn(t, "approve")
	if openBefore != stillOpen {
		t.Fatalf("before the skip the approval has %d open task(s), want %d", openBefore, stillOpen)
	}
	if before := f.onlyInstance(t); !before.IsMultiInstanceActive("approve") {
		t.Fatal("before the skip the instance is not counting the approval's runs, so the skip has no count to end")
	}
	v2, err := f.svc.CreateDefinition(f.ctx, repeatingApproval(f.project, kind, loop, keepsTheStep))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	if err := f.migrateWithApproval(t, v1, v2, nil,
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
	if instance.IsMultiInstanceActive("approve") {
		t.Errorf("the skipped approval is still counting its runs (%+v): sent back to the step, the instance would find it already running and ask nobody",
			instance.MultiInstance["approve"])
	}
	if instance.Definition == nil || instance.Definition.ID != v2 {
		t.Fatal("the instance was passed over: one run of the migration must skip the whole step and move it")
	}
	if open, ever := f.tasksOn(t, "approve"); open != 0 || ever != approvalsBefore {
		t.Fatalf("after the skip the approval has %d open task(s) and %d in all, want none open and %d in all (no new run)",
			open, ever, approvalsBefore)
	}
	if len(watcher.events) != stillOpen {
		t.Errorf("the skip announced %d withdrawal(s), want one for each of the %d run(s) still open", len(watcher.events), stillOpen)
	}
	for _, event := range watcher.events {
		if event.Node == nil || event.Node.ID != "approve" {
			t.Errorf("a withdrawal does not name the approval it took away: %+v", event.Node)
		}
	}
	if row := f.theWaiverOf(t, instance.ID, "approve"); row.Details["withdrawn"] != float64(stillOpen) {
		t.Errorf("the ledger says the skip withdrew %v task(s), want %d: %v", row.Details["withdrawn"], stillOpen, row.Details)
	}
	if open, _ := f.tasksOn(t, "record"); open != 1 {
		t.Fatalf("the step after the approval has %d open task(s), want exactly one", open)
	}
	record := f.theOpenTask(t, "after the skip")
	if err := f.svc.CompleteTask(testutils.AsOperator(f.ctx, "carol"), record.ID, "carol", nil); err != nil {
		t.Fatalf("complete the recording: %v", err)
	}
	if got := f.onlyInstance(t); got.Status != entities.ProcessCompleted || len(got.Tokens) != 0 {
		t.Fatalf("the instance is %s with %d token(s), want completed and empty", got.Status, len(got.Tokens))
	}
}
