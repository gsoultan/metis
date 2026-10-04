package bpmn_test

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/tests/testutils"
)

// tasksSection is the tasks half of a ledger row's before or after: what each
// task was, or became, by task id.
func tasksSection(t *testing.T, section map[string]any) map[string]map[string]any {
	t.Helper()
	tasks, ok := section["tasks"].(map[string]any)
	if !ok {
		t.Fatalf("the row says nothing of the tasks: %v", section)
	}
	out := make(map[string]map[string]any, len(tasks))
	for id, values := range tasks {
		of, ok := values.(map[string]any)
		if !ok {
			t.Fatalf("the row says of task %s: %v", id, values)
		}
		out[id] = of
	}
	return out
}

// BPMN 2.0.2 §13.3.2 (activity lifecycle): an activity can be withdrawn; it is
// then not completed, and the token leaves it. A waive is that, decided by an
// administrator: the holder is told the work was withdrawn, the process moves
// on, and nothing anywhere says the step was performed.
func TestAWaivedStepIsWithdrawnItsHolderToldAndTheInstanceMovesOn(t *testing.T) {
	h := newEngineHarness(t, "Waive Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, opsApproval(h.projID, "ops-waive"), nil)
	opsTask := theOpenTask(t, h, id, "opsApprove")
	before, err := h.repo.Process().Get(ctx, id)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}

	out := w.mustApply(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	if out.Replayed || out.Deviation.Kind != entities.DeviationWaive {
		t.Fatalf("the outcome: %+v", out)
	}
	if open := openIterationTasks(ctx, t, h, id, "opsApprove"); len(open) != 0 {
		t.Fatalf("the waived step still has %d open task(s)", len(open))
	}
	// Withdrawn, not completed: the row says which.
	withdrawnTask, err := h.svc.GetTask(ctx, opsTask.ID)
	if err != nil || withdrawnTask.Status != entities.TaskCanceled {
		t.Fatalf("the waived task is %q (%v), want it withdrawn", withdrawnTask.Status, err)
	}
	withdrawn := events.ofType(entities.EventTaskCanceled)
	if len(withdrawn) != 1 || withdrawn[0].Assignee != "ollie" {
		t.Fatalf("withdrawal events %+v, want one telling ollie", withdrawn)
	}
	if completed := events.ofType(entities.EventTaskCompleted); len(completed) != 0 {
		t.Fatalf("a waive raised %d TaskCompleted event(s); that is how a waived approval reads as given", len(completed))
	}
	if !h.waitingAt(ctx, t, id, "salesApprove") {
		t.Fatal("the process did not move on to the sales approval")
	}
	if tokens := tokensOn(t, h, id, "opsApprove"); tokens != 0 {
		t.Fatalf("the waived step still holds %d token(s)", tokens)
	}
	if seen := tasksEverOn(t, h, id, "salesApprove"); seen != 1 {
		t.Fatalf("the process moved on %d times, want once", seen)
	}

	rows := w.ledger(t, id)
	if len(rows) != 1 {
		t.Fatalf("the ledger holds %d rows, want one", len(rows))
	}
	row := rows[0]
	if row.ID != out.Deviation.ID {
		t.Fatalf("the outcome names row %s and the ledger holds %s", out.Deviation.ID, row.ID)
	}
	if row.Kind != entities.DeviationWaive || row.Origin != entities.DeviationOriginInPlace || row.Scope != entities.DeviationScopeTask ||
		row.Status != entities.DeviationApplied || row.Actor != "ana" || row.Reason != waiveReason ||
		row.Task == nil || row.Task.ID != opsTask.ID || row.VisitKey == "" || row.VisitKey != out.Plan.VisitKey {
		t.Fatalf("the waive's row: %+v", row)
	}
	if row.Node == nil || row.Node.ID != "opsApprove" || row.Node.Name != "Operations approve" {
		t.Fatalf("the row's step: %+v", row.Node)
	}
	if row.Project == nil || row.Project.ID != h.projID || row.Instance == nil || row.Instance.ID != id ||
		row.Definition == nil || row.Definition.ID.String() != before.DefinitionID.String() {
		t.Fatalf("the row's project, instance and version: %+v, %+v, %+v", row.Project, row.Instance, row.Definition)
	}
	was, is := tasksSection(t, row.Before), tasksSection(t, row.After)
	if len(was) != 1 || was[opsTask.ID.String()]["assignee"] != "ollie" || was[opsTask.ID.String()]["status"] != string(opsTask.Status) {
		t.Fatalf("the row says the task was %v, want ollie's, %s", was, opsTask.Status)
	}
	if len(is) != 1 || is[opsTask.ID.String()]["status"] != string(entities.TaskCanceled) {
		t.Fatalf("the row says the task became %v, want it withdrawn", is)
	}
	if _, set := row.After["variables"]; set {
		t.Fatalf("a waive that set nothing says it set %v", row.After["variables"])
	}
	if row.Details["withdrawn"] != float64(1) {
		t.Fatalf("the row's details: %v, want one task withdrawn", row.Details)
	}

	entries, err := h.svc.GetAuditLogs(ctx, id)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	var skipped []entities.AuditEntry
	for _, entry := range entries {
		if entry.Type == serviceimpl.EventTaskCompleted {
			t.Fatalf("the trail has a task_completed entry for a waived step: %+v", entry)
		}
		if entry.Type == serviceimpl.EventNodeSkipped {
			skipped = append(skipped, entry)
		}
	}
	if len(skipped) != 1 {
		t.Fatalf("the trail has %d node_skipped entries, want one", len(skipped))
	}
	entry := skipped[0]
	if entry.ID != row.AuditEntryID {
		t.Errorf("the row points at entry %s and the entry is %s", row.AuditEntryID, entry.ID)
	}
	for key, want := range map[string]any{
		"outcome": "waived", "origin": "in_place", "action": "skip", "actor": "ana", "node_id": "opsApprove",
		"reason": waiveReason, "deviation_id": row.ID.String(), "run_id": row.RunID.String(),
	} {
		if entry.Data[key] != want {
			t.Errorf("the entry's %s is %v, want %v", key, entry.Data[key], want)
		}
	}
	for _, words := range []string{"“Operations approve” was waived", "nobody performed it", "by ana", waiveReason} {
		if !strings.Contains(entry.Narrative, words) {
			t.Errorf("the entry reads %q; it does not say %q", entry.Narrative, words)
		}
	}
	for _, words := range []string{"completed", "approved", "performed by"} {
		if strings.Contains(strings.ToLower(entry.Narrative), words) {
			t.Errorf("the entry reads %q; %q is how a step somebody did reads", entry.Narrative, words)
		}
	}

	completeTaskAt(ctx, t, h, id, "salesApprove", nil)
	requireInstanceStatus(ctx, t, h, id, entities.ProcessCompleted)
}

// BPMN 2.0.2 §10.3.8 / §13.2.7, completionCondition: "the remaining instances
// are canceled". A waive is that, taken unconditionally: every open run is
// withdrawn, every run's token goes, and the process moves on once.
func TestWaivingAParallelApprovalWithdrawsEveryOpenRunAndAdvancesOnce(t *testing.T) {
	h := newEngineHarness(t, "Parallel Waive Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	w := newWaiver(h)
	ctx := h.Ctx()
	id := startApproval(t, h, approvalDefinition(h.projID, "purchase-waive-parallel", "parallel", ""), "ana", "budi", "citra")
	open := openIterationTasks(ctx, t, h, id, "approve")
	if len(open) != 3 {
		t.Fatalf("three approvers were asked and %d task(s) are open", len(open))
	}
	if err := completeAs(ctx, h, open[0], "carol", map[string]any{"decision": "yes"}); err != nil {
		t.Fatalf("complete one approval: %v", err)
	}
	w.mustApply(t, deviationCommand(entities.DeviationWaive, id, "approve", nil))
	if left := tokenIterationsOn(ctx, t, h, id, "approve"); len(left) != 0 {
		t.Fatalf("the waived step still holds tokens %v", left)
	}
	if still := openIterationTasks(ctx, t, h, id, "approve"); len(still) != 0 {
		t.Fatalf("the waived step still has %d open run(s)", len(still))
	}
	rows := w.ledger(t, id)
	if len(rows) != 1 || rows[0].Details["withdrawn"] != float64(2) {
		t.Fatalf("the ledger: %+v, want one row that withdrew 2", rows)
	}
	// Two runs withdrawn is no one task's waiver: the row names the step, and
	// each run it took.
	if rows[0].Task != nil {
		t.Errorf("a waive of two runs names one task: %+v", rows[0].Task)
	}
	was := tasksSection(t, rows[0].Before)
	if _, listed := was[open[0].ID.String()]; listed || len(was) != 2 {
		t.Errorf("the row lists %v as withdrawn; the run carol completed was not, and the other two were", was)
	}
	if told := events.ofType(entities.EventTaskCanceled); len(told) != 2 {
		t.Errorf("%d withdrawal(s) were announced, want one for each of the two open runs", len(told))
	}
	// The run carol gave stays given.
	given, err := h.svc.GetTask(ctx, open[0].ID)
	if err != nil || given.Status != entities.TaskCompleted {
		t.Errorf("the approval carol gave is %q (%v) after the waive, want it still completed", given.Status, err)
	}
	finishRecording(ctx, t, h, id)
}

func TestWaivingASequentialApprovalStartsNoFurtherRun(t *testing.T) {
	h := newEngineHarness(t, "Sequential Waive Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	id := startApproval(t, h, approvalDefinition(h.projID, "purchase-waive-sequential", "sequential", ""), "ana", "budi", "citra")
	w.mustApply(t, deviationCommand(entities.DeviationWaive, id, "approve", nil))
	if ever := tasksEverOn(t, h, id, "approve"); ever != 1 {
		t.Fatalf("the approval had %d task(s) in all, want the one that was waived and no further run", ever)
	}
	if left := tokenIterationsOn(ctx, t, h, id, "approve"); len(left) != 0 {
		t.Fatalf("the waived step still holds tokens %v", left)
	}
	finishRecording(ctx, t, h, id)
}

// BPMN 2.0.2 §10.3.3 (Manual Task): work a person does away from the system
// is somebody's work all the same, with a task in somebody's list. It is
// waived as a user task is.
func TestAManualStepIsWaivedAsWorkSomebodyDoes(t *testing.T) {
	h := newEngineHarness(t, "Manual Waive Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "ship-by-hand-waive",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "ship", Type: entities.ManualTask, Name: "Ship the parcel", Assignee: "dana"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "ship"},
			{ID: "f2", SourceRef: "ship", TargetRef: "end"},
		},
	}, nil)
	task := theOpenTask(t, h, id, "ship")
	w.mustApply(t, deviationCommand(entities.DeviationWaive, id, "ship", nil))
	requireInstanceStatus(ctx, t, h, id, entities.ProcessCompleted)
	if row := w.theWaive(t, id); row.Task == nil || row.Task.ID != task.ID {
		t.Fatalf("the waive's row names task %+v, want the manual step's", row.Task)
	}
	if told := events.ofType(entities.EventTaskCanceled); len(told) != 1 || told[0].Assignee != "dana" {
		t.Fatalf("withdrawal events %+v, want one telling dana", told)
	}
	if completed := events.ofType(entities.EventTaskCompleted); len(completed) != 0 {
		t.Fatalf("a waived manual step raised %d TaskCompleted event(s)", len(completed))
	}
}

// BPMN 2.0.2 §10.2.5 (Sub-Process): the token is on the step inside, so the
// waive names that step and moves on inside the sub-process.
func TestAWaiveInsideAnEmbeddedSubProcessMovesOnInsideIt(t *testing.T) {
	h := newEngineHarness(t, "Embedded Waive Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "embedded-waive",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "sub", Type: entities.SubProcess, Name: "Approvals", Nodes: []*entities.Node{
				{ID: "sub-start", Type: entities.StartEvent, ParentID: "sub"},
				{ID: "approve", Type: entities.UserTask, Name: "Approve inside", Assignee: "rita", ParentID: "sub",
					Properties: testutils.FormDeclaring("approved")},
				{ID: "sub-end", Type: entities.EndEvent, ParentID: "sub"},
			}, Flows: []*entities.SequenceFlow{
				{ID: "s1", SourceRef: "sub-start", TargetRef: "approve"},
				{ID: "s2", SourceRef: "approve", TargetRef: "sub-end"},
			}},
			{ID: "after", Type: entities.UserTask, Name: "After the approvals"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "sub"},
			{ID: "f2", SourceRef: "sub", TargetRef: "after"},
			{ID: "f3", SourceRef: "after", TargetRef: "end"},
		},
	}, nil)
	if plan := w.preview(t, deviationCommand(entities.DeviationWaive, id, "sub", nil)); plan.Applicable() {
		t.Fatal("waiving the sub-process itself was accepted; the work is the step inside it")
	}
	w.mustApply(t, deviationCommand(entities.DeviationWaive, id, "approve", nil))
	if !h.waitingAt(ctx, t, id, "after") {
		t.Fatal("the sub-process did not finish after its one step was waived")
	}
	if seen := tasksEverOn(t, h, id, "after"); seen != 1 {
		t.Fatalf("the process left the sub-process %d times, want once", seen)
	}
	completeTaskAt(ctx, t, h, id, "after", nil)
	requireInstanceStatus(ctx, t, h, id, entities.ProcessCompleted)
}

// BPMN 2.0.2 §10.2.5 (Ad-Hoc Sub-Process): its completion condition is re-read
// when a step inside finishes. The waive must say what the step counts as for
// that condition, and finishing it re-reads it.
func TestAWaiveInsideAnAdHocSubProcessRereadsItsCompletionCondition(t *testing.T) {
	h := newEngineHarness(t, "AdHoc Waive Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	def := adHocDefinition("claim-research-waive", "reviewsDone >= 1")
	def.Project = &entities.Project{ID: h.projID}
	h.deploy(t, &def)
	id, err := h.svc.StartProcess(ctx, h.projID, "claim-research-waive", map[string]any{"reviewsDone": 0})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := h.svc.ActivateTask(ctx, id, "research", "call-customer"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	plan := w.preview(t, deviationCommand(entities.DeviationWaive, id, "call-customer", nil))
	if _, listed := pointOfKind(plan, "research", entities.DecisionPointCompletionCondition); plan.Applicable() || !listed ||
		!refusalMentions(plan, "“Research the claim” decides from reviewsDone", "supplying reviewsDone") {
		t.Fatalf("the plan does not refuse for the completion condition's missing reviewsDone: points %+v, refusals:%s",
			plan.DecisionPoints, lines(plan.Refusals))
	}
	// The value the instance already holds would leave the sub-process
	// waiting; the waiver's is the one read.
	w.mustApply(t, deviationCommand(entities.DeviationWaive, id, "call-customer", map[string]any{"reviewsDone": 1}))
	if !h.waitingAt(ctx, t, id, "decide") {
		t.Fatal("the completion condition was met by the waiver's value and the process did not move on")
	}
	row := w.theWaive(t, id)
	before, _ := row.Before["variables"].(map[string]any)
	after, _ := row.After["variables"].(map[string]any)
	if before["reviewsDone"] != float64(0) || after["reviewsDone"] != float64(1) {
		t.Errorf("the row says reviewsDone went from %v to %v, want 0 to 1", before["reviewsDone"], after["reviewsDone"])
	}
}

// BPMN 2.0.2 §10.2.6 (Call Activity): the work is in the called process. The
// call activity itself is not waived; the step inside the child is, and the
// child ending resumes its caller.
func TestAWaiveInACalledProcessResumesItsCaller(t *testing.T) {
	h := newEngineHarness(t, "Called Waive Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "supplier-review",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the supplier", Assignee: "rita"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "c1", SourceRef: "start", TargetRef: "review"},
			{ID: "c2", SourceRef: "review", TargetRef: "end"},
		},
	})
	parent := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "onboarding-waive",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "check", Type: entities.CallActivity, Name: "Check the supplier", Properties: map[string]any{"called_process_key": "supplier-review"}},
			{ID: "sign", Type: entities.UserTask, Name: "Sign the contract"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "p1", SourceRef: "start", TargetRef: "check"},
			{ID: "p2", SourceRef: "check", TargetRef: "sign"},
			{ID: "p3", SourceRef: "sign", TargetRef: "end"},
		},
	}, nil)
	child := theOneCalledBy(t, h, parent)
	if plan := w.preview(t, deviationCommand(entities.DeviationWaive, parent, "check", nil)); plan.Applicable() || !refusalMentions(plan, child.String()) {
		t.Fatalf("waiving the call activity: refusals %q, want it refused naming the called instance", plan.Refusals)
	}
	w.mustApply(t, deviationCommand(entities.DeviationWaive, child, "review", nil))
	requireInstanceStatus(ctx, t, h, child, entities.ProcessCompleted)
	if !h.waitingAt(ctx, t, parent, "sign") {
		t.Fatal("the caller did not resume when its called process ended")
	}
	if seen := tasksEverOn(t, h, parent, "sign"); seen != 1 {
		t.Fatalf("the caller was resumed %d times, want once", seen)
	}
	if rows := w.ledger(t, child); len(rows) != 1 {
		t.Fatalf("the child's ledger holds %d rows, want the waive", len(rows))
	}
	// The waive was of the step in the called process. The caller was resumed
	// by that process ending, as by any ending: nothing was waived on it.
	if rows := w.ledger(t, parent); len(rows) != 0 {
		t.Fatalf("the caller's ledger holds %d row(s) for a waive made in the process it called", len(rows))
	}
	completeTaskAt(ctx, t, h, parent, "sign", nil)
	requireInstanceStatus(ctx, t, h, parent, entities.ProcessCompleted)
}

// Review Focus 4. A waive whose advance cannot route — the gateway after it
// has no branch for the value given and no default — rolls back whole: the
// task is open again in the same hands, the value is not set, and nothing is
// recorded. TestASkipThatCannotAdvanceLeavesTheStepToBeDone pins the same for
// a migration skip.
//
// The withdrawal is announced inside the transaction, so a watcher has heard
// of it by the time it is undone. What is asked here is what was kept: every
// row of every table.
func TestAWaiveThatCannotAdvanceLeavesTheStepToBeDone(t *testing.T) {
	h := newEngineHarness(t, "Unroutable Waive Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "unroutable-waive",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the claim", Assignee: "rita", Properties: testutils.FormDeclaring("verdict")},
			{ID: "decide", Type: entities.ExclusiveGateway, Name: "Verdict?"},
			{ID: "accepted", Type: entities.EndEvent},
			{ID: "rejected", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "decide"},
			{ID: "yes", SourceRef: "decide", TargetRef: "accepted", Condition: "verdict = accept"},
			{ID: "no", SourceRef: "decide", TargetRef: "rejected", Condition: "verdict = reject"},
		},
	}, nil)
	before := everyRow(t, h)

	command := deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"verdict": "maybe"})
	if plan := w.preview(t, command); !plan.Applicable() {
		t.Fatalf("the plan refuses, so the advance is never tried and this proves nothing:%s", lines(plan.Refusals))
	}
	out, err := w.apply(t, command)
	if err == nil {
		t.Fatal("a waive whose gateway could not choose reported success")
	}
	if !strings.Contains(err.Error(), "Review the claim") {
		t.Errorf("the error %q does not name the step it could not waive", err)
	}
	if out.Applied || out.Deviation != nil {
		t.Errorf("a waive that failed answered as though it had acted: %+v", out)
	}
	open := openIterationTasks(ctx, t, h, id, "review")
	if len(open) != 1 || open[0].AssigneeUsername() != "rita" {
		t.Fatalf("after a waive that could not advance the review's open work is %+v, want rita's one task back", open)
	}
	if rows := w.ledger(t, id); len(rows) != 0 {
		t.Fatalf("a rolled-back waive left %d ledger row(s)", len(rows))
	}
	instance := requireInstanceStatus(ctx, t, h, id, entities.ProcessActive)
	if _, set := instance.Variables["verdict"]; set {
		t.Errorf("a rolled-back waive left verdict set to %v", instance.Variables["verdict"])
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("a waive that could not advance changed %v", changed)
	}

	// And the step is still there to do, or to waive with a value that routes.
	w.mustApply(t, deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"verdict": "accept"}))
	requireInstanceStatus(ctx, t, h, id, entities.ProcessCompleted)
}
