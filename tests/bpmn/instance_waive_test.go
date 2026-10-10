package bpmn_test

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories/models"
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
	h.recordsAsProductionDoes()
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
	withdrawals := 0
	for _, entry := range entries {
		// Neither the task service's entry for a completion nor the one the
		// audit observer writes when a completion is announced.
		if entry.Type == serviceimpl.EventTaskCompleted || entry.Type == entities.EventTaskCompleted {
			t.Fatalf("the trail has a %s entry for a waived step: %+v", entry.Type, entry)
		}
		if entry.Type == serviceimpl.EventNodeSkipped {
			skipped = append(skipped, entry)
		}
		if entry.Type == entities.EventTaskCanceled {
			withdrawals++
		}
	}
	if len(skipped) != 1 {
		t.Fatalf("the trail has %d node_skipped entries, want one", len(skipped))
	}
	if withdrawals != 1 {
		t.Errorf("the trail records %d withdrawal(s) of the step's task, want the one the audit observer writes", withdrawals)
	}
	notices, err := h.repo.Notification().ListByUser(ctx, "ollie")
	if err != nil {
		t.Fatalf("read ollie's notifications: %v", err)
	}
	toldWithdrawn := 0
	for _, notice := range notices {
		if notice.Title == "A task was withdrawn" {
			toldWithdrawn++
		}
	}
	if toldWithdrawn != 1 {
		t.Errorf("ollie has %d notice(s) that a task was withdrawn, want one: %+v", toldWithdrawn, notices)
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
	// The whole sentence: it is what the timeline shows and what the docs
	// quote. (The reason given here ends with a full stop of its own.)
	if want := "“Operations approve” was waived — nobody performed it — by ana. Reason: " + waiveReason + "."; entry.Narrative != want {
		t.Errorf("the entry reads\n  %s\nwant\n  %s", entry.Narrative, want)
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
	// Once, though two runs were ended: one token and one task on what follows.
	if tokens, tasks := tokensOn(t, h, id, "record"), tasksEverOn(t, h, id, "record"); tokens != 1 || tasks != 1 {
		t.Fatalf("after two runs were waived the next step holds %d token(s) and has had %d task(s), want one of each", tokens, tasks)
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

// unroutable is start → Review the claim (declares verdict) → a gateway with a
// flow for "accept" and one for "reject", and no default.
func unroutable(h engineHarness, key string, gateway entities.NodeType, gatewayName string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the claim", Assignee: "rita", Properties: testutils.FormDeclaring("verdict")},
			{ID: "decide", Type: gateway, Name: gatewayName},
			{ID: "accepted", Type: entities.EndEvent},
			{ID: "rejected", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "decide"},
			{ID: "yes", SourceRef: "decide", TargetRef: "accepted", Condition: "verdict = accept"},
			{ID: "no", SourceRef: "decide", TargetRef: "rejected", Condition: "verdict = reject"},
		},
	}
}

// Review Focus 4. A waive whose advance cannot route — the gateway after it
// has no branch for the value given and no default — rolls back whole: the
// task is open again in the same hands, the value is not set, and nothing is
// recorded. TestASkipThatCannotAdvanceLeavesTheStepToBeDone pins the same for
// a migration skip.
//
// It is the administrator's to fix — a value was given, and it fits nothing —
// so it is answered as that, in words that name the gateway and say that
// nothing was changed. Not as a failure of the server, which their client
// would send again.
//
// The withdrawal is announced inside the transaction, so a watcher has heard
// of it by the time it is undone. What is asked here is what was kept: every
// row of every table, with the observers that write in production writing.
func TestAWaiveThatCannotAdvanceLeavesTheStepToBeDone(t *testing.T) {
	for name, shape := range map[string]struct {
		gateway     entities.NodeType
		gatewayName string
		called      string
	}{
		"an exclusive gateway":         {entities.ExclusiveGateway, "Verdict?", "Verdict?"},
		"an inclusive gateway":         {entities.InclusiveGateway, "Verdict?", "Verdict?"},
		"a gateway nobody gave a name": {entities.ExclusiveGateway, "", "decide"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newEngineHarness(t, "Unroutable Waive Project")
			h.recordsAsProductionDoes()
			w := newWaiver(h)
			ctx := h.Ctx()
			id := w.start(t, unroutable(h, "unroutable-waive", shape.gateway, shape.gatewayName), nil)

			command := deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"verdict": "maybe"})
			if plan := w.preview(t, command); !plan.Applicable() {
				t.Fatalf("the plan refuses, so the advance is never tried and this proves nothing:%s", lines(plan.Refusals))
			}
			asked := w.ask(t, command)
			before := everyRow(t, h)
			out, err := w.approve(asked)
			want := apierr.Invalidf("The values given fit no way out of “%s”, so the waive was not applied and nothing was changed. "+
				"Preview again and give a value one of its branches accepts. The request is still waiting: reject it, and the waive can be asked for again.", shape.called)
			if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != want.Error() {
				t.Fatalf("a waive whose gateway could not choose: got\n  %v\nwant it refused as the caller's to fix, saying exactly\n  %v", err, want)
			}
			if out.Applied || out.Deviation != nil {
				t.Errorf("a waive that failed answered as though it had acted: %+v", out)
			}
			open := openIterationTasks(ctx, t, h, id, "review")
			if len(open) != 1 || open[0].AssigneeUsername() != "rita" {
				t.Fatalf("after a waive that could not advance the review's open work is %+v, want rita's one task back", open)
			}
			rows := w.ledger(t, id)
			if len(rows) != 1 || rows[0].Status != entities.DeviationPendingApproval {
				t.Fatalf("after an approval that could not advance the ledger holds %+v; the request must still be waiting, unchanged", rows)
			}
			instance := requireInstanceStatus(ctx, t, h, id, entities.ProcessActive)
			if _, set := instance.Variables["verdict"]; set {
				t.Errorf("a rolled-back waive left verdict set to %v", instance.Variables["verdict"])
			}
			if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
				t.Fatalf("a waive that could not advance changed %v", changed)
			}

			// The request that could not be approved still waits. The step is
			// still there to do, and asking for it to be waived with another
			// value is told that one is waiting — never left to find out.
			other := w.previewed(t, deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"verdict": "accept"}))
			if _, err := w.asking.DeviateInstance(w.ctx, other); !errors.Is(err, apierr.ErrInvalidArgument) ||
				!strings.Contains(err.Error(), "already waiting for approval") {
				t.Fatalf("a second request for a step that has one waiting: %v, want it told so", err)
			}
			// With no request waiting, a value that routes is waived through.
			second := w.start(t, unroutable(h, "unroutable-waive", shape.gateway, shape.gatewayName), nil)
			w.mustApply(t, deviationCommand(entities.DeviationWaive, second, "review", map[string]any{"verdict": "accept"}))
			requireInstanceStatus(ctx, t, h, second, entities.ProcessCompleted)
		})
	}
}

// What follows a waived step can fail for reasons that are nobody's request:
// here a business-rule step consults a decision nobody stored (the plan says
// it could not read it, and does not refuse). The read of that decision
// answers "not found" — and an instance that exists, asked about by somebody
// who may ask, must not be answered as not found, nor as a request they could
// put right. It is the server's trouble: the words are kept, the class is not.
func TestAWaiveWhoseAdvanceFailsForReasonsOfItsOwnIsNotBlamedOnTheCaller(t *testing.T) {
	h := newEngineHarness(t, "Waive Advance Failure Project")
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "advance-failure",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the claim", Assignee: "rita", Properties: testutils.FormDeclaring("approved")},
			{ID: "policy", Type: entities.BusinessRuleTask, Name: "Apply the refund policy", Properties: map[string]any{"decision_key": "no-such-decision"}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "policy"},
			{ID: "f3", SourceRef: "policy", TargetRef: "end"},
		},
	}, nil)

	command := deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"approved": true})
	if plan := w.preview(t, command); !plan.Applicable() {
		t.Fatalf("the plan refuses, so the advance is never tried and this proves nothing:%s", lines(plan.Refusals))
	}
	asked := w.ask(t, command)
	before := everyRow(t, h)
	out, err := w.approve(asked)
	if err == nil {
		t.Fatal("a waive whose next step could not run reported success")
	}
	for class, kind := range map[string]error{"not found": apierr.ErrNotFound, "an invalid argument": apierr.ErrInvalidArgument, "forbidden": apierr.ErrForbidden} {
		if errors.Is(err, kind) {
			t.Errorf("the failure is answered as %s: %v", class, err)
		}
	}
	for _, words := range []string{"Review the claim", "no-such-decision"} {
		if !strings.Contains(err.Error(), words) {
			t.Errorf("the error %q does not say %q", err, words)
		}
	}
	if out.Applied || out.Deviation != nil {
		t.Errorf("a waive that failed answered as though it had acted: %+v", out)
	}
	if open := openIterationTasks(ctx, t, h, id, "review"); len(open) != 1 {
		t.Fatalf("after the failure the review has %d open task(s), want the one", len(open))
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("a waive whose advance failed changed %v", changed)
	}
}

// supplierCheck is the called process, start → Review the supplier (declares
// approved) → end, and its caller, "Supplier onboarding": start → Check the
// supplier (calls it) → Supplier approved? → sign | drop.
func supplierCheck(t *testing.T, h engineHarness, called, caller string) {
	t.Helper()
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: called, Name: "Supplier review",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the supplier", Assignee: "rita", Properties: testutils.FormDeclaring("approved")},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "c1", SourceRef: "start", TargetRef: "review"},
			{ID: "c2", SourceRef: "review", TargetRef: "end"},
		},
	})
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: caller, Name: "Supplier onboarding",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "check", Type: entities.CallActivity, Name: "Check the supplier", Properties: map[string]any{"called_process_key": called}},
			{ID: "decide", Type: entities.ExclusiveGateway, Name: "Supplier approved?"},
			{ID: "sign", Type: entities.UserTask, Name: "Sign the contract"},
			{ID: "drop", Type: entities.UserTask, Name: "Drop the supplier"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "p1", SourceRef: "start", TargetRef: "check"},
			{ID: "p2", SourceRef: "check", TargetRef: "decide"},
			{ID: "yes", SourceRef: "decide", TargetRef: "sign", Condition: "approved"},
			{ID: "no", SourceRef: "decide", TargetRef: "drop", Condition: "approved = false"},
			{ID: "p3", SourceRef: "sign", TargetRef: "end"},
			{ID: "p4", SourceRef: "drop", TargetRef: "end"},
		},
	})
}

// BPMN 2.0.2 §10.2.6 (Call Activity): when the called process ends, its
// results go back to the caller, which goes on from the call activity. The
// caller's process is another definition: a plan for a waive in the called
// instance reads the called instance's process and not the caller's. So what
// the caller decides from the results is not known here, and the plan says
// so, naming the caller and the step that waits — it does not refuse, and it
// does not stay silent.
//
// When the caller then cannot decide — its gateway reads a value the waived
// step would have set, and the waiver gave none — the waive is undone whole,
// in both instances, and the administrator is told which gateway it was and
// that nothing was changed. They are not told to give "a value one of its
// branches accepts": the gateway is in a process their preview did not show.
func TestAWaiveInACalledProcessSaysItsCallerWasNotRead(t *testing.T) {
	h := newEngineHarness(t, "Called Waive Caller Project")
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	ctx := h.Ctx()
	supplierCheck(t, h, "supplier-review-read", "onboarding-read")
	parent, err := h.svc.StartProcess(ctx, h.projID, "onboarding-read", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	child := theOneCalledBy(t, h, parent)

	bare := deviationCommand(entities.DeviationWaive, child, "review", nil)
	plan := w.preview(t, bare)
	warning := callerNotReadWarning
	if !said(plan.Warnings, warning) {
		t.Fatalf("the plan does not warn\n  %s\nits warnings:%s", warning, lines(plan.Warnings))
	}
	if !plan.Applicable() {
		t.Fatalf("a waive is refused for what its caller might read:%s", lines(plan.Refusals))
	}
	// A hold and a cancel hand nothing to the caller, and are told nothing of it here.
	if hold := w.preview(t, deviationCommand(entities.DeviationHold, child, "review", nil)); said(hold.Warnings, warning) {
		t.Error("a hold, which ends nothing, is warned of what the caller decides from the results")
	}

	// Applied with no value: the caller's gateway has nothing to decide from.
	asked := w.ask(t, bare)
	before := everyRow(t, h)
	out, err := w.approve(asked)
	want := apierr.Invalidf("“Supplier approved?”, in the process that started this one, had no way out for the result, " +
		"so the waive was not applied and nothing was changed. The request is still waiting: reject it, and the waive can be asked for again.")
	if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != want.Error() {
		t.Fatalf("a waive whose caller could not decide: got\n  %v\nwant it refused, saying exactly\n  %v", err, want)
	}
	if out.Applied || out.Deviation != nil {
		t.Errorf("a waive that failed answered as though it had acted: %+v", out)
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("a waive whose caller could not decide changed %v", changed)
	}
	requireInstanceStatus(ctx, t, h, child, entities.ProcessActive)
	if !h.waitingAt(ctx, t, child, "review") {
		t.Fatal("the called instance is no longer waiting at the review")
	}

	// The request that could not be approved still waits, and a second one
	// for the step, with the value the caller reads, is told so.
	other := w.previewed(t, deviationCommand(entities.DeviationWaive, child, "review", map[string]any{"approved": false}))
	if _, err := w.asking.DeviateInstance(w.ctx, other); !errors.Is(err, apierr.ErrInvalidArgument) ||
		!strings.Contains(err.Error(), "already waiting for approval") {
		t.Fatalf("a second request for a step that has one waiting: %v, want it told so", err)
	}
	// With no request waiting and the value the caller reads, the caller
	// decides from it: a second caller, started the same way.
	secondParent, err := h.svc.StartProcess(ctx, h.projID, "onboarding-read", nil)
	if err != nil {
		t.Fatalf("start a second caller: %v", err)
	}
	secondChild := theOneCalledBy(t, h, secondParent)
	w.mustApply(t, deviationCommand(entities.DeviationWaive, secondChild, "review", map[string]any{"approved": false}))
	requireInstanceStatus(ctx, t, h, secondChild, entities.ProcessCompleted)
	if !h.waitingAt(ctx, t, secondParent, "drop") || h.waitingAt(ctx, t, secondParent, "sign") {
		t.Fatal("the caller did not decide from what the waiver counted as")
	}
}

// callerNotReadWarning is what a plan for a waive in an instance of
// supplierCheck's called process says of its caller.
const callerNotReadWarning = "This instance was started by “Supplier onboarding” at “Check the supplier”, " +
	"which receives its results when it ends and was not read. " +
	"Where that process decides on a value this step would have set and you give none, " +
	"it decides on the value it already holds, or undoes the waive if it holds none. Check that process before applying."

// Review Focus 3, one process up. A caller hands its values to the process it
// calls and takes them back when that process ends. So when the caller
// already holds a value for a field the waived step would have set, and the
// waiver gives none, the caller's gateway decides on the value it held: the
// waive is applied, and the caller goes down the branch of an answer nobody
// gave this time. The caller's process is not read by a plan for the called
// instance, so nothing refuses this. What the plan does is say so, in the
// warning — and this pins both: what happens, and that the administrator was
// told it could.
func TestAWaiveInACalledProcessLetsItsCallerDecideOnAValueItAlreadyHolds(t *testing.T) {
	h := newEngineHarness(t, "Called Waive Stale Caller Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	supplierCheck(t, h, "supplier-review-stale", "onboarding-stale")
	parent, err := h.svc.StartProcess(ctx, h.projID, "onboarding-stale", map[string]any{"approved": true})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	child := theOneCalledBy(t, h, parent)
	if held := variablesOf(t, h, child)["approved"]; held != true {
		t.Fatalf("the called instance was handed approved = %v by its caller, want true; this test rests on that", held)
	}

	out := w.mustApply(t, deviationCommand(entities.DeviationWaive, child, "review", nil))
	if !said(out.Plan.Warnings, callerNotReadWarning) {
		t.Fatalf("the plan that was applied did not warn\n  %s\nits warnings:%s", callerNotReadWarning, lines(out.Plan.Warnings))
	}
	requireInstanceStatus(ctx, t, h, child, entities.ProcessCompleted)
	if !h.waitingAt(ctx, t, parent, "sign") || h.waitingAt(ctx, t, parent, "drop") {
		t.Fatal("the caller did not decide on the value it already held")
	}
	// The record of the waive says nothing was set: the answer the caller
	// took was not the waiver's.
	if row := w.theWaive(t, child); row.After["variables"] != nil {
		t.Errorf("the waive is recorded as setting %v; it set nothing", row.After["variables"])
	}
}

// What follows a waived step may be a step that calls another process, and
// that process starts inside the same advance. A gateway there with no way
// out undoes the waive too. It is neither this instance's gateway nor its
// caller's, and is said to be neither: nobody is told to supply a value for a
// process their preview only warned it had not read.
func TestAWaiveThatAProcessItGoesOnToCallCannotFollowIsUndone(t *testing.T) {
	h := newEngineHarness(t, "Waive Into Called Project")
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	ctx := h.Ctx()
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "stock-check", Name: "Stock check",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "in-hand", Type: entities.ExclusiveGateway, Name: "Stock in hand?"},
			{ID: "ship", Type: entities.UserTask, Name: "Ship it"},
			{ID: "order", Type: entities.UserTask, Name: "Order it"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "c1", SourceRef: "start", TargetRef: "in-hand"},
			{ID: "yes", SourceRef: "in-hand", TargetRef: "ship", Condition: "stock = plenty"},
			{ID: "no", SourceRef: "in-hand", TargetRef: "order", Condition: "stock = none"},
			{ID: "c2", SourceRef: "ship", TargetRef: "end"},
			{ID: "c3", SourceRef: "order", TargetRef: "end"},
		},
	})
	id := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "order-with-stock-check",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "count", Type: entities.UserTask, Name: "Count the stock", Assignee: "rita", Properties: testutils.FormDeclaring("stock")},
			{ID: "check", Type: entities.CallActivity, Name: "Check the stock", Properties: map[string]any{"called_process_key": "stock-check"}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "count"},
			{ID: "f2", SourceRef: "count", TargetRef: "check"},
			{ID: "f3", SourceRef: "check", TargetRef: "end"},
		},
	}, nil)

	command := deviationCommand(entities.DeviationWaive, id, "count", map[string]any{"stock": "some"})
	if plan := w.preview(t, command); !plan.Applicable() {
		t.Fatalf("the plan refuses, so the advance is never tried and this proves nothing:%s", lines(plan.Refusals))
	}
	asked := w.ask(t, command)
	before := everyRow(t, h)
	out, err := w.approve(asked)
	want := apierr.Invalidf("“Stock in hand?”, in another process this waive reached, had no way out, " +
		"so the waive was not applied and nothing was changed. The request is still waiting: reject it, and the waive can be asked for again.")
	if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != want.Error() {
		t.Fatalf("a waive the called process could not follow: got\n  %v\nwant it refused, saying exactly\n  %v", err, want)
	}
	if out.Applied || out.Deviation != nil {
		t.Errorf("a waive that failed answered as though it had acted: %+v", out)
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("a waive the called process could not follow changed %v", changed)
	}
	if called, err := h.svc.ListSubProcesses(ctx, id); err != nil || len(called) != 0 {
		t.Fatalf("the undone waive left %d called instance(s) behind (%v)", len(called), err)
	}

	// The request that could not be approved still waits, and a second one
	// for the step, with a value the called process has a branch for, is told so.
	other := w.previewed(t, deviationCommand(entities.DeviationWaive, id, "count", map[string]any{"stock": "none"}))
	if _, err := w.asking.DeviateInstance(w.ctx, other); !errors.Is(err, apierr.ErrInvalidArgument) ||
		!strings.Contains(err.Error(), "already waiting for approval") {
		t.Fatalf("a second request for a step that has one waiting: %v, want it told so", err)
	}
	// With no request waiting and a value the called process has a branch
	// for, the waive is applied and that process is running: a second
	// instance, started the same way.
	second, err := h.svc.StartProcess(ctx, h.projID, "order-with-stock-check", nil)
	if err != nil {
		t.Fatalf("start a second instance: %v", err)
	}
	w.mustApply(t, deviationCommand(entities.DeviationWaive, second, "count", map[string]any{"stock": "none"}))
	if !h.waitingAt(ctx, t, theOneCalledBy(t, h, second), "order") {
		t.Fatal("the called process did not take the branch of the value the waiver gave")
	}
}

// A waive that gives no value can meet a gateway with no way out as well: one
// that reads something the waived step never set. Nothing was given, so
// nothing given is said to fit badly, and nobody is told to give a value the
// step's form does not have: the instance held no way out, and nothing was
// changed.
func TestAWaiveThatGivesNothingAndCannotAdvanceSaysWhatTheInstanceHeld(t *testing.T) {
	h := newEngineHarness(t, "Unroutable Bare Waive Project")
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	id := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "unroutable-bare-waive",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the claim", Assignee: "rita", Properties: testutils.FormDeclaring("verdict")},
			{ID: "size", Type: entities.ExclusiveGateway, Name: "Large claim?"},
			{ID: "large", Type: entities.EndEvent},
			{ID: "small", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "size"},
			{ID: "yes", SourceRef: "size", TargetRef: "large", Condition: "amount > 1000"},
			{ID: "no", SourceRef: "size", TargetRef: "small", Condition: "amount <= 1000"},
		},
	}, nil)

	command := deviationCommand(entities.DeviationWaive, id, "review", nil)
	if plan := w.preview(t, command); !plan.Applicable() {
		t.Fatalf("the plan refuses, so the advance is never tried and this proves nothing:%s", lines(plan.Refusals))
	}
	asked := w.ask(t, command)
	before := everyRow(t, h)
	_, err := w.approve(asked)
	want := apierr.Invalidf("“Large claim?” had no way out for the values this instance holds, so the waive was not applied and nothing was changed. " +
		"The request is still waiting: reject it, and the waive can be asked for again.")
	if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != want.Error() {
		t.Fatalf("a waive that gave nothing and could not advance: got\n  %v\nwant exactly\n  %v", err, want)
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("a waive that could not advance changed %v", changed)
	}
}

// A caller that has ended receives nothing: the warning is for a caller that
// is still waiting. A migration's cancel ends a caller without looking at
// what it called; here the row is written through the repository, as suspend
// does, because nothing in place can end a caller under a called instance.
func TestAWaiveInACalledProcessWhoseCallerHasEndedIsNotWarnedOfIt(t *testing.T) {
	h := newEngineHarness(t, "Called Waive Ended Caller Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	supplierCheck(t, h, "supplier-review-ended", "onboarding-ended")
	parent, err := h.svc.StartProcess(ctx, h.projID, "onboarding-ended", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	child := theOneCalledBy(t, h, parent)
	row, err := h.repo.Process().Get(ctx, parent)
	if err != nil {
		t.Fatalf("read the caller: %v", err)
	}
	row.Status, row.Tokens = models.ProcessCancelled, nil
	if err := h.repo.Process().Update(ctx, row); err != nil {
		t.Fatalf("end the caller: %v", err)
	}
	plan := w.preview(t, deviationCommand(entities.DeviationWaive, child, "review", nil))
	// The whole list: whose work is taken, and nothing of a caller.
	want := []string{"“Review the supplier” is with rita, who will be told it was withdrawn."}
	if !reflect.DeepEqual(plan.Warnings, want) {
		t.Errorf("a waive whose caller has ended warns:%s\nwant only:%s", lines(plan.Warnings), lines(want))
	}
}

// BPMN 2.0.2 §10.2.5 (Ad-Hoc Sub-Process): a step inside finishing re-reads
// the completion condition, and when it does not hold the sub-process goes on
// waiting. A waived step is ended all the same — its task withdrawn, its
// token gone, its value kept — and the instance stays in the sub-process.
func TestAWaiveInsideAnAdHocSubProcessThatIsNotYetDoneLeavesItWaiting(t *testing.T) {
	h := newEngineHarness(t, "AdHoc Unmet Waive Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	def := adHocDefinition("claim-research-unmet", "reviewsDone >= 2")
	def.Project = &entities.Project{ID: h.projID}
	h.deploy(t, &def)
	id, err := h.svc.StartProcess(ctx, h.projID, "claim-research-unmet", map[string]any{"reviewsDone": 0})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := h.svc.ActivateTask(ctx, id, "research", "call-customer"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	w.mustApply(t, deviationCommand(entities.DeviationWaive, id, "call-customer", map[string]any{"reviewsDone": 1}))

	if open := openIterationTasks(ctx, t, h, id, "call-customer"); len(open) != 0 {
		t.Fatalf("the waived step still has %d open task(s)", len(open))
	}
	if tokens := tokensOn(t, h, id, "call-customer"); tokens != 0 {
		t.Fatalf("the waived step still holds %d token(s)", tokens)
	}
	instance := requireInstanceStatus(ctx, t, h, id, entities.ProcessActive)
	if instance.Variables["reviewsDone"] != float64(1) && instance.Variables["reviewsDone"] != 1 {
		t.Errorf("the instance holds reviewsDone = %v, want what the waiver counted as", instance.Variables["reviewsDone"])
	}
	if tokens := tokensOn(t, h, id, "research"); tokens != 1 {
		t.Fatalf("the sub-process holds %d token(s), want it still waiting", tokens)
	}
	if h.waitingAt(ctx, t, id, "decide") || tasksEverOn(t, h, id, "decide") != 0 {
		t.Fatal("the process left the sub-process though its completion condition does not hold")
	}
	w.theWaive(t, id)

	// The sub-process is still one somebody can work in: the next step done
	// meets the condition, and the process moves on once.
	if err := h.svc.ActivateTask(ctx, id, "research", "call-customer"); err != nil {
		t.Fatalf("activate again: %v", err)
	}
	completeTaskAt(ctx, t, h, id, "call-customer", map[string]any{"reviewsDone": 2})
	if !h.waitingAt(ctx, t, id, "decide") || tasksEverOn(t, h, id, "decide") != 1 {
		t.Fatal("with the condition met the process did not move on once")
	}
}

// A step's name is its author's to choose, and may be longer than the ledger
// keeps. The plan, the ledger row and the trail entry name it the same way:
// its first 255 characters, cut between characters.
func TestAStepWithAVeryLongNameIsNamedAlikeInThePlanTheLedgerAndTheTrail(t *testing.T) {
	h := newEngineHarness(t, "Long Name Waive Project")
	w := newWaiver(h)
	long := strings.Repeat("é", 300)
	def := opsApproval(h.projID, "ops-long-name")
	def.Nodes[1].Name = long
	id := w.start(t, def, nil)

	out := w.mustApply(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	want := strings.Repeat("é", 255)
	if out.Plan.NodeName != want {
		t.Errorf("the plan names the step with %d characters, want its first 255", utf8.RuneCountInString(out.Plan.NodeName))
	}
	row := w.theWaive(t, id)
	if row.Node == nil || row.Node.Name != want {
		t.Errorf("the ledger names the step %+v, want its first 255 characters", row.Node)
	}
	entries := skippedEntries(t, h, id)
	if len(entries) != 1 {
		t.Fatalf("the trail has %d node_skipped entries, want one", len(entries))
	}
	if entries[0].Node == nil || entries[0].Node.Name != want {
		t.Errorf("the trail names the step with %d characters, want the 255 the ledger keeps", utf8.RuneCountInString(entries[0].Node.Name))
	}
	if !strings.Contains(entries[0].Narrative, "“"+want+"” was waived") {
		t.Errorf("the entry's sentence does not name the step as the ledger does: %q", entries[0].Narrative)
	}
}

// A waive ends every run of a repeating approval, and a step done once for
// each of many people has a task for each. The act takes every one: each run
// is withdrawn and each holder told once. Its record counts them all and names
// the two hundred with the lowest ids, as a cancel's does — a row is read
// whole, by the apply's reply, by every replay and by the ledger's route, and
// how many runs a step has is the instance's to say.
func TestAWaiveOfMoreRunsThanARowNamesWithdrawsThemAll(t *testing.T) {
	h := newEngineHarness(t, "Waive Many Runs Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	ctx := h.Ctx()
	const inAll, named = 205, 200
	approvers := make([]any, inAll)
	for i := range approvers {
		approvers[i] = fmt.Sprintf("approver%03d", i)
	}
	id := startApproval(t, h, approvalDefinition(h.projID, "waive-many-runs", "parallel", ""), approvers...)
	open := openIterationTasks(ctx, t, h, id, "approve")
	if len(open) != inAll {
		t.Fatalf("the step has %d open tasks; this test needs %d", len(open), inAll)
	}
	// Everybody takes their task, so every withdrawal has somebody to tell.
	holderOf := make(map[uuid.UUID]string, inAll)
	for i, task := range open {
		holder := fmt.Sprintf("approver%03d", i)
		if err := h.svc.ClaimTask(testutils.AsOperator(ctx, holder), task.ID, holder); err != nil {
			t.Fatalf("%s claims: %v", holder, err)
		}
		holderOf[task.ID] = holder
	}

	out := w.mustApply(t, deviationCommand(entities.DeviationWaive, id, "approve", nil))
	if left := openIterationTasks(ctx, t, h, id, "approve"); len(left) != 0 || tokensOn(t, h, id, "approve") != 0 || !h.waitingAt(ctx, t, id, "record") {
		t.Fatalf("%d run(s) are still open and %d token(s) still on the step after the waive", len(left), tokensOn(t, h, id, "approve"))
	}
	told := toldOfWithdrawal(events)
	if len(told) != inAll {
		t.Fatalf("%d holder(s) were told of a withdrawal, want each of %d", len(told), inAll)
	}
	for holder, times := range told {
		if times != 1 {
			t.Errorf("%s was told %d times, want once", holder, times)
		}
	}

	row := w.theWaive(t, id)
	was, is := tasksSection(t, row.Before), tasksSection(t, row.After)
	if row.Details["withdrawn"] != float64(inAll) || row.Details["tasks_listed"] != float64(named) || len(was) != named || len(is) != named || row.Task != nil {
		t.Fatalf("the row counts %v withdrawn and %v listed, and names %d before and %d after; want %d withdrawn and %d named",
			row.Details["withdrawn"], row.Details["tasks_listed"], len(was), len(is), inAll, named)
	}
	slices.SortFunc(open, func(a, b entities.Task) int { return bytes.Compare(a.ID[:], b.ID[:]) })
	for i, task := range open {
		recorded, has := was[task.ID.String()]
		if has != (i < named) {
			t.Fatalf("task %d in the order of their ids is named on the row: %v; the row names the first %d", i+1, has, named)
		}
		if has && (recorded["assignee"] != holderOf[task.ID] || recorded["status"] != string(entities.TaskClaimed) || is[task.ID.String()]["status"] != string(entities.TaskCanceled)) {
			t.Errorf("the row says of %s's task: %v, then %v", holderOf[task.ID], recorded, is[task.ID.String()])
		}
		// Named on the row or not, the task's own row says who held it.
		if now, err := h.svc.GetTask(ctx, task.ID); err != nil || now.Status != entities.TaskCanceled || now.AssigneeUsername() != holderOf[task.ID] {
			t.Fatalf("%s's task is %q with %q (%v), want it withdrawn and still theirs", holderOf[task.ID], now.Status, now.AssigneeUsername(), err)
		}
	}
	// The reply carries the same row, not a longer one.
	if replied := tasksSection(t, out.Deviation.Before); len(replied) != named {
		t.Errorf("the apply answered a row naming %d tasks, want %d", len(replied), named)
	}
}

// A step marked as a control is waived like any other, and afterwards the
// instance's own list of completed steps counts it as passed. So the waive
// says what it is: the plan warns before the apply, and the ledger row and the
// trail entry carry the mark, so that nobody has to join the row to the
// definition to see that a control was not performed. A step that is not a
// control has neither the warning nor the mark.
func TestAWaiveOfAControlSaysSo(t *testing.T) {
	h := newEngineHarness(t, "Waive Control Project")
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	const warning = "“Operations approve” is marked as a control. Waiving it is recorded as a control that was not performed."

	control := opsApproval(h.projID, "ops-control")
	control.Nodes[1].Properties["compliance_relevant"] = true
	marked := w.start(t, control, nil)
	plain := w.start(t, opsApproval(h.projID, "ops-no-control"), nil)

	waive := func(id uuid.UUID) entities.DeviationCommand {
		return deviationCommand(entities.DeviationWaive, id, "opsApprove", map[string]any{"approved": true})
	}
	wantWarnings := []string{warning, "“Operations approve” is with ollie, who will be told it was withdrawn."}
	if plan := w.preview(t, waive(marked)); !plan.Applicable() || !reflect.DeepEqual(plan.Warnings, wantWarnings) {
		t.Fatalf("the plan for a waive of a control: refusals:%s\nwarnings:%s\nwant the warnings:%s", lines(plan.Refusals), lines(plan.Warnings), lines(wantWarnings))
	}
	if plan := w.preview(t, waive(plain)); !plan.Applicable() || !reflect.DeepEqual(plan.Warnings, wantWarnings[1:]) {
		t.Fatalf("the plan for a waive of a step that is not a control: warnings:%s\nwant only:%s", lines(plan.Warnings), lines(wantWarnings[1:]))
	}
	// A cancel and a hold do not skip the control: they say nothing of it.
	for _, kind := range []entities.DeviationKind{entities.DeviationCancel, entities.DeviationHold} {
		if plan := w.preview(t, deviationCommand(kind, marked, "opsApprove", nil)); said(plan.Warnings, warning) {
			t.Errorf("a %s at a control warns that it is waived:%s", kind, lines(plan.Warnings))
		}
	}

	w.mustApply(t, waive(marked))
	w.mustApply(t, waive(plain))
	row, entry := w.theWaive(t, marked), theEntryOf(t, h, marked, serviceimpl.EventNodeSkipped)
	if row.Details["control"] != true || entry.Data["control"] != true {
		t.Errorf("the waive of a control: the row's details %v and the entry's data %v; want each to carry control: true", row.Details, entry.Data)
	}
	row, entry = w.theWaive(t, plain), theEntryOf(t, h, plain, serviceimpl.EventNodeSkipped)
	if _, has := row.Details["control"]; has {
		t.Errorf("the waive of a step that is not a control carries the mark on its row: %v", row.Details)
	}
	if _, has := entry.Data["control"]; has {
		t.Errorf("the waive of a step that is not a control carries the mark on its entry: %v", entry.Data)
	}
}
