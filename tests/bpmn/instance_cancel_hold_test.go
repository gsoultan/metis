package bpmn_test

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// entriesOfType is the entries of one type on an instance's trail.
func entriesOfType(t *testing.T, h engineHarness, id uuid.UUID, eventType string) []entities.AuditEntry {
	t.Helper()
	entries, err := h.svc.GetAuditLogs(h.Ctx(), id)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	var of []entities.AuditEntry
	for _, entry := range entries {
		if entry.Type == eventType {
			of = append(of, entry)
		}
	}
	return of
}

// theEntryOf is the one entry of a type on an instance's trail, and fails the
// test when the trail has any other number of them.
func theEntryOf(t *testing.T, h engineHarness, id uuid.UUID, eventType string) entities.AuditEntry {
	t.Helper()
	of := entriesOfType(t, h, id, eventType)
	if len(of) != 1 {
		t.Fatalf("the trail has %d %s entries, want exactly one", len(of), eventType)
	}
	return of[0]
}

// withdrawalNotices is how many times somebody was left a notice that a task
// of theirs was withdrawn.
func withdrawalNotices(t *testing.T, h engineHarness, user string) int {
	t.Helper()
	notices, err := h.repo.Notification().ListByUser(h.Ctx(), user)
	if err != nil {
		t.Fatalf("read %s's notifications: %v", user, err)
	}
	told := 0
	for _, notice := range notices {
		if notice.Title == "A task was withdrawn" {
			told++
		}
	}
	return told
}

// toldOfWithdrawal is who the engine told that a task was withdrawn, and how
// many times each.
func toldOfWithdrawal(events *eventLog) map[string]int {
	told := map[string]int{}
	for _, event := range events.ofType(entities.EventTaskCanceled) {
		told[event.Assignee]++
	}
	return told
}

// tablesChanged names the tables two readings of everyRow disagree on, in
// order.
func tablesChanged(before, after map[string]string) []string {
	var changed []string
	for table, rows := range after {
		if before[table] != rows {
			changed = append(changed, table)
		}
	}
	slices.Sort(changed)
	return changed
}

// statusIn is the status a ledger row says an instance had, or came to have.
func statusIn(section map[string]any) any {
	instance, _ := section["instance"].(map[string]any)
	return instance["status"]
}

// requireNoStep fails unless a ledger row and its trail entry name no step:
// what the record of an instance closed while it waited nowhere must not
// invent.
func requireNoStep(t *testing.T, row entities.Deviation, entry entities.AuditEntry) {
	t.Helper()
	if row.Node != nil {
		t.Fatalf("the row names a step the instance was not at: %+v", row.Node)
	}
	if _, named := entry.Data["node_id"]; named || (entry.Node != nil && entry.Node.ID != "") || strings.Contains(entry.Narrative, "ended at") {
		t.Fatalf("the entry names a step the instance was not at: %q %+v %+v", entry.Narrative, entry.Data, entry.Node)
	}
}

// BPMN 2.0.2 §13.3.2 (activity lifecycle) and §13.5.6 (terminating a process
// instance): ended from outside, an instance stops where it stands, and the
// activities still running in it are withdrawn, not completed. A cancel in
// place is that, decided by an administrator: the work is taken back and its
// holder told, nothing moves on, and the ledger and the trail say who ended
// it, where it stood and why.
func TestCancellingInPlaceEndsTheInstanceAndWithdrawsItsWork(t *testing.T) {
	h := newEngineHarness(t, "Cancel In Place Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, opsApproval(h.projID, "ops-cancel"), nil)
	opsTask := theOpenTask(t, h, id, "opsApprove")
	before, err := h.repo.Process().Get(ctx, id)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}

	out := w.mustApply(t, deviationCommand(entities.DeviationCancel, id, "opsApprove", nil))
	if out.Replayed || out.Deviation.Kind != entities.DeviationCancel {
		t.Fatalf("the outcome: %+v", out)
	}
	instance := requireInstanceStatus(ctx, t, h, id, entities.ProcessCancelled)
	if len(instance.Tokens) != 0 {
		t.Fatalf("a cancelled instance holds %d token(s)", len(instance.Tokens))
	}
	if open := openIterationTasks(ctx, t, h, id, "opsApprove"); len(open) != 0 {
		t.Fatalf("%d task(s) still open on a cancelled instance", len(open))
	}
	// Withdrawn, not completed, and nothing after it was started.
	withdrawnTask, err := h.svc.GetTask(ctx, opsTask.ID)
	if err != nil || withdrawnTask.Status != entities.TaskCanceled {
		t.Fatalf("the task of a cancelled instance is %q (%v), want it withdrawn", withdrawnTask.Status, err)
	}
	if told := toldOfWithdrawal(events); !reflect.DeepEqual(told, map[string]int{"ollie": 1}) {
		t.Fatalf("withdrawals were announced to %v, want ollie told once", told)
	}
	if completed := events.ofType(entities.EventTaskCompleted); len(completed) != 0 {
		t.Fatalf("a cancel raised %d TaskCompleted event(s)", len(completed))
	}
	if seen := tasksEverOn(t, h, id, "salesApprove"); seen != 0 {
		t.Fatalf("a cancelled instance moved on: %d task(s) at the next step", seen)
	}

	rows := w.ledger(t, id)
	if len(rows) != 1 {
		t.Fatalf("the ledger holds %d rows, want one", len(rows))
	}
	row := rows[0]
	if row.ID != out.Deviation.ID {
		t.Fatalf("the outcome names row %s and the ledger holds %s", out.Deviation.ID, row.ID)
	}
	if row.Kind != entities.DeviationCancel || row.Scope != entities.DeviationScopeInstance || row.Origin != entities.DeviationOriginInPlace ||
		row.Status != entities.DeviationApplied || row.Actor != "ana" || row.Reason != waiveReason ||
		row.VisitKey == "" || row.VisitKey != out.Plan.VisitKey {
		t.Fatalf("the cancel's row: %+v", row)
	}
	if row.Node == nil || row.Node.ID != "opsApprove" || row.Node.Name != "Operations approve" {
		t.Fatalf("the row's step: %+v", row.Node)
	}
	if row.Project == nil || row.Project.ID != h.projID || row.Instance == nil || row.Instance.ID != id ||
		row.Definition == nil || row.Definition.ID.String() != before.DefinitionID.String() {
		t.Fatalf("the row's project, instance and version: %+v, %+v, %+v", row.Project, row.Instance, row.Definition)
	}
	if statusIn(row.Before) != "active" || statusIn(row.After) != "cancelled" {
		t.Fatalf("the row says the instance was %v and became %v, want active and cancelled", statusIn(row.Before), statusIn(row.After))
	}
	was, is := tasksSection(t, row.Before), tasksSection(t, row.After)
	if len(was) != 1 || was[opsTask.ID.String()]["assignee"] != "ollie" || was[opsTask.ID.String()]["status"] != string(opsTask.Status) {
		t.Fatalf("the row says the task was %v, want ollie's, %s", was, opsTask.Status)
	}
	if len(is) != 1 || is[opsTask.ID.String()]["status"] != string(entities.TaskCanceled) {
		t.Fatalf("the row says the task became %v, want it withdrawn", is)
	}
	// As a migration's cancel records it: the one task it took is named.
	if row.Task == nil || row.Task.ID != opsTask.ID {
		t.Fatalf("the row's task: %+v, want the one task the cancel withdrew", row.Task)
	}
	if !reflect.DeepEqual(row.Details, map[string]any{"withdrawn": float64(1)}) {
		t.Fatalf("the row's details: %v, want one task withdrawn and nothing else", row.Details)
	}

	entry := theEntryOf(t, h, id, serviceimpl.EventInstanceCancelled)
	if entry.ID != row.AuditEntryID {
		t.Errorf("the row points at entry %s and the entry is %s", row.AuditEntryID, entry.ID)
	}
	for key, want := range map[string]any{
		"action": "cancel", "origin": "in_place", "actor": "ana", "node_id": "opsApprove",
		"reason": waiveReason, "deviation_id": row.ID.String(), "run_id": row.RunID.String(),
	} {
		if entry.Data[key] != want {
			t.Errorf("the entry's %s is %v, want %v", key, entry.Data[key], want)
		}
	}
	if want := "This instance was ended at “Operations approve” by ana. Reason: " + waiveReason + "."; entry.Narrative != want {
		t.Errorf("the entry reads\n  %s\nwant\n  %s", entry.Narrative, want)
	}
	if entry.Node == nil || entry.Node.ID != "opsApprove" || entry.Node.Name != "Operations approve" {
		t.Errorf("the entry's step: %+v", entry.Node)
	}
	// A cancel is not a step waived, and the withdrawal has its own entry and
	// its own notice, written as production writes them.
	if skipped := skippedEntries(t, h, id); len(skipped) != 0 {
		t.Errorf("the trail says a step of a cancelled instance was waived: %+v", skipped)
	}
	if withdrawals := entriesOfType(t, h, id, entities.EventTaskCanceled); len(withdrawals) != 1 {
		t.Errorf("the trail records %d withdrawal(s) of the instance's task, want the one the audit observer writes", len(withdrawals))
	}
	if told := withdrawalNotices(t, h, "ollie"); told != 1 {
		t.Errorf("ollie has %d notice(s) that a task was withdrawn, want one", told)
	}
}

// awaitingPayment is start → a message the process waits for → Confirm the
// order → end.
func awaitingPayment(projectID uuid.UUID, key string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID}, Key: key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "awaitPayment", Type: entities.IntermediateCatchEvent, Name: "Wait for the payment",
				Properties: map[string]any{"message_name": "PaymentReceived", "correlation_key": "${orderId}"}},
			{ID: "confirm", Type: entities.UserTask, Name: "Confirm the order"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "m1", SourceRef: "start", TargetRef: "awaitPayment"},
			{ID: "m2", SourceRef: "awaitPayment", TargetRef: "confirm"},
			{ID: "m3", SourceRef: "confirm", TargetRef: "end"},
		},
	}
}

// An instance that will never run again cannot honour what it was waiting
// for: a cancel takes its subscriptions with it, so a message that arrives
// afterwards finds nothing to resume. Nobody held any work, and the record
// says that none was withdrawn.
func TestCancellingInPlaceStopsTheInstanceWaitingForAMessage(t *testing.T) {
	h := newEngineHarness(t, "Cancel Waiting Event Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, awaitingPayment(h.projID, "cancel-awaiting"), map[string]any{"orderId": "A-1"})
	waiting, err := h.repo.Subscription().ListByInstance(ctx, id)
	if err != nil || len(waiting) != 1 {
		t.Fatalf("the instance waits for %d message(s) (err %v); this test needs one", len(waiting), err)
	}

	w.mustApply(t, deviationCommand(entities.DeviationCancel, id, "awaitPayment", nil))
	requireInstanceStatus(ctx, t, h, id, entities.ProcessCancelled)
	if left, err := h.repo.Subscription().ListByInstance(ctx, id); err != nil || len(left) != 0 {
		t.Fatalf("a cancelled instance still waits for %d message(s) (err %v)", len(left), err)
	}
	rows := w.ledger(t, id)
	if len(rows) != 1 || rows[0].Node == nil || rows[0].Node.Name != "Wait for the payment" || rows[0].Task != nil {
		t.Fatalf("the cancel's ledger: %+v", rows)
	}
	if _, tasks := rows[0].Before["tasks"]; tasks || !reflect.DeepEqual(rows[0].Details, map[string]any{"withdrawn": float64(0)}) {
		t.Fatalf("the row of a cancel that withdrew nothing: before %v, details %v", rows[0].Before, rows[0].Details)
	}
	// The payment arrives after all. Whatever the sender is told, nothing is
	// started for an instance that was ended.
	_ = h.engine.SendMessage(ctx, h.projID, "PaymentReceived", "A-1", nil)
	requireInstanceStatus(ctx, t, h, id, entities.ProcessCancelled)
	if seen := tasksEverOn(t, h, id, "confirm"); seen != 0 {
		t.Fatalf("a message moved a cancelled instance on: %d task(s) at the next step", seen)
	}
}

// A hold changes where an instance is visible, not what it is: one incident,
// one ledger row and one trail entry, and nothing else anywhere. Holding it
// again for the same visit replays rather than raising a second incident, and
// the work is still there for its holder to do.
func TestHoldingInPlaceRaisesOneIncidentAndChangesNothingElse(t *testing.T) {
	h := newEngineHarness(t, "Hold In Place Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, opsApproval(h.projID, "ops-hold"), nil)
	opsTask := theOpenTask(t, h, id, "opsApprove")
	cmd := w.previewed(t, deviationCommand(entities.DeviationHold, id, "opsApprove", nil))
	before, raised := everyRow(t, h), events.count()

	first, err := w.svc.DeviateInstance(w.ctx, cmd)
	if err != nil || !first.Applied || first.Replayed || first.Deviation == nil {
		t.Fatalf("hold: %+v, %v", first, err)
	}
	held := everyRow(t, h)
	if changed, want := tablesChanged(before, held), []string{"audit_logs", "incidents", "instance_deviations"}; !reflect.DeepEqual(changed, want) {
		t.Fatalf("a hold changed %v, want only %v: the incident, its ledger row and its trail entry", changed, want)
	}
	if now := events.count(); now != raised {
		t.Fatalf("a hold raised %d event(s); it takes nothing from anybody", now-raised)
	}
	requireInstanceStatus(ctx, t, h, id, entities.ProcessActive)
	if still := theOpenTask(t, h, id, "opsApprove"); still.ID != opsTask.ID || still.AssigneeUsername() != "ollie" || still.Status != opsTask.Status {
		t.Fatalf("a hold disturbed the work: %+v", still)
	}

	incidents, err := h.svc.ListIncidents(ctx, id)
	if err != nil || len(incidents) != 1 {
		t.Fatalf("incidents: %d (err %v), want exactly one", len(incidents), err)
	}
	incident := incidents[0]
	if want := "held at “Operations approve” by ana: " + waiveReason; incident.Error != want {
		t.Errorf("the incident reads\n  %s\nwant\n  %s", incident.Error, want)
	}
	if incident.Status != entities.IncidentOpen || incident.Node == nil || incident.Node.ID != "opsApprove" {
		t.Errorf("the incident: %+v, want it open on the step", incident)
	}

	rows := w.ledger(t, id)
	if len(rows) != 1 || rows[0].ID != first.Deviation.ID {
		t.Fatalf("the hold's ledger: %+v", rows)
	}
	row := rows[0]
	if row.Kind != entities.DeviationHold || row.Scope != entities.DeviationScopeInstance || row.Origin != entities.DeviationOriginInPlace ||
		row.Status != entities.DeviationApplied || row.Actor != "ana" || row.Reason != waiveReason ||
		row.VisitKey != cmd.VisitKey || row.Task != nil ||
		row.Node == nil || row.Node.ID != "opsApprove" || row.Node.Name != "Operations approve" {
		t.Fatalf("the hold's row: %+v", row)
	}
	wantAfter := map[string]any{"incident": map[string]any{"id": incident.ID.String(), "status": "open"}}
	if len(row.Before) != 0 || !reflect.DeepEqual(row.After, wantAfter) || len(row.Details) != 0 {
		t.Fatalf("the hold's row says before %v, after %v, details %v; want only the incident, after", row.Before, row.After, row.Details)
	}

	entry := theEntryOf(t, h, id, serviceimpl.EventInstanceHeld)
	if entry.ID != row.AuditEntryID {
		t.Errorf("the row points at entry %s and the entry is %s", row.AuditEntryID, entry.ID)
	}
	for key, want := range map[string]any{
		"action": "hold", "origin": "in_place", "actor": "ana", "node_id": "opsApprove", "reason": waiveReason,
		"deviation_id": row.ID.String(), "run_id": row.RunID.String(), "incident_id": incident.ID.String(),
	} {
		if entry.Data[key] != want {
			t.Errorf("the entry's %s is %v, want %v", key, entry.Data[key], want)
		}
	}
	if want := "This instance was held at “Operations approve” by ana and raised as an incident for somebody to decide. Reason: " +
		waiveReason + "."; entry.Narrative != want {
		t.Errorf("the entry reads\n  %s\nwant\n  %s", entry.Narrative, want)
	}

	// Sent again, it is answered with what it did and does nothing.
	second, err := w.svc.DeviateInstance(w.ctx, cmd)
	if err != nil || !second.Applied || !second.Replayed || second.Deviation == nil || second.Deviation.ID != row.ID {
		t.Fatalf("holding again for the same visit: %+v, %v; want a replay of the same row", second, err)
	}
	if changed := tablesThatDiffer(held, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("a replayed hold changed %v", changed)
	}
	// The same visit asked about again is the same plan, and says it is held.
	again := w.preview(t, deviationCommand(entities.DeviationHold, id, "opsApprove", nil))
	if again.VisitKey != cmd.VisitKey || !said(again.Warnings, "“Operations approve” already has an open incident; the hold will use it.") {
		t.Errorf("a preview of a visit already held: the same key: %v, warnings:%s", again.VisitKey == cmd.VisitKey, lines(again.Warnings))
	}
	other := cmd
	other.Reason = "a different reason, sent later"
	if _, err := w.svc.DeviateInstance(w.ctx, other); !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "this instance was already held by ana") {
		t.Errorf("another hold of the same visit: %v, want it refused as already held by ana", err)
	}
	if incidents, err := h.svc.ListIncidents(ctx, id); err != nil || len(incidents) != 1 {
		t.Fatalf("after the hold was sent again: %d incident(s) (err %v), want the one", len(incidents), err)
	}

	// Held is not stopped: ollie can still do the work.
	if err := completeAs(ctx, h, opsTask, "ollie", map[string]any{"approved": true}); err != nil {
		t.Fatalf("ollie completes the step of a held instance: %v", err)
	}
	if !h.waitingAt(ctx, t, id, "salesApprove") {
		t.Fatal("a held instance did not move on when its step was done")
	}
}

// Plan Ruling 16. An in-place hold is a fresh act for the visit it names, and
// is recorded even when the step already has an incident open: it uses that
// one and raises no second. Here the step is held, one of its three runs is
// then finished — different work, so a different visit — and it is held again.
func TestAHoldOfAStepAlreadyHeldUsesItsIncidentAndIsStillRecorded(t *testing.T) {
	h := newEngineHarness(t, "Hold Again Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	id := startApproval(t, h, approvalDefinition(h.projID, "hold-again", "parallel", ""), "ana", "budi", "citra")
	hold := deviationCommand(entities.DeviationHold, id, "approve", nil)
	first := w.mustApply(t, hold)

	runs := openIterationTasks(ctx, t, h, id, "approve")
	if err := completeAs(ctx, h, runs[0], "carol", map[string]any{"decision": "yes"}); err != nil {
		t.Fatalf("finish one run: %v", err)
	}
	plan := w.preview(t, hold)
	if plan.VisitKey == first.Plan.VisitKey {
		t.Fatal("a run was finished and the hold's key did not change; this test needs a second visit")
	}
	if !plan.Applicable() || !said(plan.Warnings, "“Approve the purchase” already has an open incident; the hold will use it.") {
		t.Fatalf("the plan of a hold where one is open: refusals:%s\nwarnings:%s", lines(plan.Refusals), lines(plan.Warnings))
	}
	second := w.mustApply(t, hold)
	if second.Replayed || second.Deviation.ID == first.Deviation.ID {
		t.Fatalf("the second hold: %+v, want an act of its own", second)
	}

	incidents, err := h.svc.ListIncidents(ctx, id)
	if err != nil || len(incidents) != 1 {
		t.Fatalf("incidents: %d (err %v), want the one the first hold raised", len(incidents), err)
	}
	rows := w.ledger(t, id)
	if len(rows) != 2 {
		t.Fatalf("the ledger holds %d rows, want a row for each hold", len(rows))
	}
	wantAfter := map[string]any{"incident": map[string]any{"id": incidents[0].ID.String(), "status": "open"}}
	for _, row := range rows {
		if row.Kind != entities.DeviationHold || !reflect.DeepEqual(row.After, wantAfter) {
			t.Errorf("a hold's row: %s after %v, want both holds to name the one incident", row.Kind, row.After)
		}
	}
	if held := entriesOfType(t, h, id, serviceimpl.EventInstanceHeld); len(held) != 2 ||
		held[0].Data["incident_id"] != incidents[0].ID.String() || held[1].Data["incident_id"] != incidents[0].ID.String() {
		t.Errorf("the trail's hold entries: %+v, want two naming the one incident", held)
	}
}

// Design §7.4, rules 11 and 12, at the apply. A called instance that is
// waiting is part of a larger process, and an instance waiting on one it
// started cannot be ended under it: an apply of either cancel is refused with
// what its preview said, and nothing is changed. A hold needs nothing further,
// so either can be held.
func TestCancelIsRefusedOnACalledInstanceAndAroundAnActiveOne(t *testing.T) {
	h := newEngineHarness(t, "Cancel Called Project")
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	ctx := h.Ctx()
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "supplier-review-cancel",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the supplier", Assignee: "rita"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{{ID: "c1", SourceRef: "start", TargetRef: "review"}, {ID: "c2", SourceRef: "review", TargetRef: "end"}},
	})
	parent := w.start(t, callerOf(h.projID, "onboarding-cancel", "supplier-review-cancel"), nil)
	child := theOneCalledBy(t, h, parent)
	before := everyRow(t, h)

	for what, refused := range map[string]struct {
		command entities.DeviationCommand
		says    []string
	}{
		"cancelling around an active called instance": {deviationCommand(entities.DeviationCancel, parent, "haveItChecked", nil),
			[]string{"is waiting on 1 process(es) it started", child.String()}},
		"cancelling a called instance that is waiting": {deviationCommand(entities.DeviationCancel, child, "review", nil),
			[]string{"started by another process", parent.String()}},
	} {
		plan := w.preview(t, refused.command)
		if plan.Applicable() || !refusalMentions(plan, refused.says...) {
			t.Fatalf("%s: the plan's refusals:%s", what, lines(plan.Refusals))
		}
		out, err := w.apply(t, refused.command)
		if !errors.Is(err, apierr.ErrInvalidArgument) || out.Applied || out.Deviation != nil {
			t.Fatalf("%s, applied: %+v, %v; want it refused as something the caller can fix", what, out, err)
		}
		for _, words := range refused.says {
			if !strings.Contains(err.Error(), words) {
				t.Errorf("%s, applied: told %q; it does not say %q", what, err, words)
			}
		}
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("refused cancels changed %v", changed)
	}

	for what, command := range map[string]entities.DeviationCommand{
		"holding the called instance": deviationCommand(entities.DeviationHold, child, "review", nil),
		"holding its caller":          deviationCommand(entities.DeviationHold, parent, "haveItChecked", nil),
	} {
		out := w.mustApply(t, command)
		if incidents, err := h.svc.ListIncidents(ctx, command.InstanceID); err != nil || len(incidents) != 1 || out.Deviation.Kind != entities.DeviationHold {
			t.Fatalf("%s: %d incident(s) (err %v), row %+v", what, len(incidents), err, out.Deviation)
		}
	}
	// Held, both run on: the called instance ends and resumes its caller.
	if err := completeAs(ctx, h, theOpenTask(t, h, child, "review"), "rita", nil); err != nil {
		t.Fatalf("rita completes the step of a held instance: %v", err)
	}
	requireInstanceStatus(ctx, t, h, child, entities.ProcessCompleted)
	requireInstanceStatus(ctx, t, h, parent, entities.ProcessCompleted)
}

// Rulings addendum §11. Whatever a preview saw is asked again of the locked
// row. An instance that ended between a preview and its apply is not held and
// not waived: no incident, no withdrawal, no row.
func TestAnApplyOnAnInstanceThatEndedSinceThePreviewDoesNothing(t *testing.T) {
	h := newEngineHarness(t, "Ended Since Preview Project")
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, opsApproval(h.projID, "ops-ended"), nil)
	stale := map[entities.DeviationKind]entities.DeviationCommand{}
	for _, kind := range []entities.DeviationKind{entities.DeviationHold, entities.DeviationWaive} {
		stale[kind] = w.previewed(t, deviationCommand(kind, id, "opsApprove", nil))
	}
	w.mustApply(t, deviationCommand(entities.DeviationCancel, id, "opsApprove", nil))
	ended := everyRow(t, h)

	for kind, cmd := range stale {
		want := fmt.Sprintf("this instance is cancelled, so it can no longer be %s; preview again",
			map[entities.DeviationKind]string{entities.DeviationHold: "held", entities.DeviationWaive: "waived"}[kind])
		out, err := w.svc.DeviateInstance(w.ctx, cmd)
		if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), want) {
			t.Errorf("a %s applied after the instance was cancelled: %v, want %q", kind, err, want)
		}
		if out.Applied || out.Replayed || out.Deviation != nil {
			t.Errorf("a %s applied after the instance was cancelled answered %+v", kind, out)
		}
	}
	if changed := tablesThatDiffer(ended, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("applies on an instance that had ended changed %v", changed)
	}
	if incidents, err := h.svc.ListIncidents(ctx, id); err != nil || len(incidents) != 0 {
		t.Fatalf("a hold of an instance that had ended raised %d incident(s) (err %v)", len(incidents), err)
	}
	if rows := w.ledger(t, id); len(rows) != 1 || rows[0].Kind != entities.DeviationCancel {
		t.Fatalf("the ledger: %+v, want the cancel and nothing else", rows)
	}
}

// The other way an instance leaves what was previewed: its holder does the
// step. A cancel and a hold previewed before that are for work that is no
// longer there, and each is told to preview again; the instance is where the
// holder's completion left it.
func TestACancelOrAHoldOfAnInstanceThatMovedSinceThePreviewIsRefused(t *testing.T) {
	h := newEngineHarness(t, "Moved Since Preview Project")
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, opsApproval(h.projID, "ops-moved-on"), nil)
	cancel := w.previewed(t, deviationCommand(entities.DeviationCancel, id, "opsApprove", nil))
	hold := w.previewed(t, deviationCommand(entities.DeviationHold, id, "opsApprove", nil))
	if err := completeAs(ctx, h, theOpenTask(t, h, id, "opsApprove"), "ollie", map[string]any{"approved": true}); err != nil {
		t.Fatalf("ollie completes: %v", err)
	}
	moved := everyRow(t, h)

	for what, cmd := range map[string]entities.DeviationCommand{"a cancel": cancel, "a hold": hold} {
		out, err := w.svc.DeviateInstance(w.ctx, cmd)
		if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "this instance has moved since you previewed it; preview again") {
			t.Errorf("%s applied after the step was done: %v, want it told to preview again", what, err)
		}
		if out.Applied || out.Replayed || out.Deviation != nil {
			t.Errorf("%s applied after the step was done answered %+v", what, out)
		}
	}
	if changed := tablesThatDiffer(moved, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("applies for work that was no longer there changed %v", changed)
	}
	requireInstanceStatus(ctx, t, h, id, entities.ProcessActive)
	if !h.waitingAt(ctx, t, id, "salesApprove") {
		t.Fatal("the instance is not where the completion left it")
	}
}

// Rulings addendum §10. An instance that is active and waiting nowhere has
// nothing in the product that could end it. A cancel is the one act that
// needs no step: it closes the instance, says in a warning that it was not
// waiting at any step, and is recorded without a step the instance was not
// at. A waive and a hold still need one.
//
// The instance is reached the way production reaches it (nothingFollows): a
// step with no way out, completed.
func TestCancellingInPlaceClosesAnInstanceThatHoldsNothing(t *testing.T) {
	h := newEngineHarness(t, "Cancel Nothing Left Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, nothingFollows(h.projID, "nothing-left"), nil)
	finishTheOnlyStep(t, h, id)

	for _, kind := range []entities.DeviationKind{entities.DeviationWaive, entities.DeviationHold, entities.DeviationCancel} {
		at := deviationCommand(kind, id, "check", nil)
		if plan := w.preview(t, at); plan.Applicable() || !refusalMentions(plan, "not waiting at", "Check the order") {
			t.Fatalf("a %s at the step the instance has left: refusals %q", kind, plan.Refusals)
		}
		if out, err := w.apply(t, at); !errors.Is(err, apierr.ErrInvalidArgument) || out.Applied {
			t.Fatalf("a %s at the step the instance has left, applied: %+v, %v", kind, out, err)
		}
	}

	closing := deviationCommand(entities.DeviationCancel, id, "", nil)
	plan := w.preview(t, closing)
	if !plan.Applicable() || plan.NodeID != "" || plan.NodeName != "" || plan.VisitKey == "" || plan.Scope != entities.DeviationScopeInstance {
		t.Fatalf("the plan for an instance with nothing left: %+v", plan)
	}
	if !said(plan.Warnings, "This instance is not waiting at any step. Cancelling it closes it.") {
		t.Fatalf("the warnings do not say the instance is not waiting at any step:%s", lines(plan.Warnings))
	}
	raised := events.count()

	cmd := closing
	cmd.VisitKey = plan.VisitKey
	cmd.DryRun = false
	out, err := w.svc.DeviateInstance(w.ctx, cmd)
	if err != nil || !out.Applied || out.Replayed || out.Deviation == nil {
		t.Fatalf("closing an instance with nothing left: %+v, %v", out, err)
	}
	if closed := requireInstanceStatus(ctx, t, h, id, entities.ProcessCancelled); len(closed.Tokens) != 0 {
		t.Fatalf("the closed instance holds %d token(s)", len(closed.Tokens))
	}
	if now := events.count(); now != raised {
		t.Errorf("closing an instance nobody held work on raised %d event(s)", now-raised)
	}
	rows := w.ledger(t, id)
	if len(rows) != 1 {
		t.Fatalf("the ledger holds %d rows, want one", len(rows))
	}
	row := rows[0]
	if row.ID != out.Deviation.ID || row.Kind != entities.DeviationCancel || row.Scope != entities.DeviationScopeInstance ||
		row.Origin != entities.DeviationOriginInPlace || row.Status != entities.DeviationApplied ||
		row.VisitKey != plan.VisitKey || row.Actor != "ana" || row.Reason != waiveReason || row.Task != nil ||
		statusIn(row.Before) != "active" || statusIn(row.After) != "cancelled" {
		t.Fatalf("the cancel's row: %+v", row)
	}
	if !reflect.DeepEqual(row.Details, map[string]any{"withdrawn": float64(0)}) {
		t.Fatalf("the row's details: %v, want no task withdrawn", row.Details)
	}
	entry := theEntryOf(t, h, id, serviceimpl.EventInstanceCancelled)
	requireNoStep(t, row, entry)
	if want := "This instance was ended by ana while it was not waiting at any step. Reason: " + waiveReason + "."; entry.Narrative != want {
		t.Errorf("the entry reads\n  %s\nwant\n  %s", entry.Narrative, want)
	}
	for key, want := range map[string]any{
		"action": "cancel", "origin": "in_place", "actor": "ana", "reason": waiveReason,
		"deviation_id": row.ID.String(), "run_id": row.RunID.String(),
	} {
		if entry.Data[key] != want {
			t.Errorf("the entry's %s is %v, want %v", key, entry.Data[key], want)
		}
	}
	if entry.ID != row.AuditEntryID {
		t.Errorf("the row points at entry %s and the entry is %s", row.AuditEntryID, entry.ID)
	}

	// Sent again, it replays: the instance is closed once and recorded once.
	closedOnce := everyRow(t, h)
	again, err := w.svc.DeviateInstance(w.ctx, cmd)
	if err != nil || !again.Applied || !again.Replayed || again.Deviation == nil || again.Deviation.ID != out.Deviation.ID {
		t.Fatalf("closing it again: %+v, %v; want a replay of the same row", again, err)
	}
	if again.Plan.NodeID != "" || again.Plan.NodeName != "" || again.Plan.VisitKey != plan.VisitKey {
		t.Errorf("the replay's plan %+v does not say what was replayed", again.Plan)
	}
	if changed := tablesThatDiffer(closedOnce, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("a replayed cancel changed %v", changed)
	}
}

// nothingFollowsAStepReachedTwice is start → fork → Check the order, entered
// by both of the fork's flows, and nothing after it. The first of its two
// tasks to be finished takes both tokens off the step and leaves the instance
// active and waiting nowhere — with the second task still open under it.
func nothingFollowsAStepReachedTwice(projectID uuid.UUID, key string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID}, Key: key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "fork", Type: entities.ParallelGateway},
			{ID: "check", Type: entities.UserTask, Name: "Check the order", Assignee: "rita"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "fork"},
			{ID: "f2", SourceRef: "fork", TargetRef: "check"},
			{ID: "f3", SourceRef: "fork", TargetRef: "check"},
		},
	}
}

// finishOneOfTwo finishes one of the two tasks of a step reached twice, checks
// that the step then holds no token though the other task is still open, and
// returns that task.
func finishOneOfTwo(t *testing.T, h engineHarness, id uuid.UUID) entities.Task {
	t.Helper()
	ctx := h.Ctx()
	open := openIterationTasks(ctx, t, h, id, "check")
	if len(open) != 2 {
		t.Fatalf("the step has %d open task(s); this fixture needs the engine to have reached it twice", len(open))
	}
	if err := h.svc.CompleteTask(ctx, open[0].ID, "rita", nil); err != nil {
		t.Fatalf("complete one of the two: %v", err)
	}
	if on, left := tokensOn(t, h, id, "check"), openIterationTasks(ctx, t, h, id, "check"); on != 0 || len(left) != 1 || left[0].ID != open[1].ID {
		t.Fatalf("after one completion the step holds %d token(s) and %d open task(s); this fixture needs none and the other task", on, len(left))
	}
	return open[1]
}

// Plan Ruling 23 (rulings addendum §13). A task can be open on a step its
// instance no longer waits at, and nothing but a cancel takes it back: no
// step can be named for it. Reached the way production reaches it — a step
// entered twice at once gives up both its tokens when the first of its two
// tasks is finished. A cancel withdraws that task with the rest and tells its
// holder, whether the instance still waits somewhere or holds nothing at all.
func TestCancellingInPlaceWithdrawsATaskTheInstanceNoLongerWaitsFor(t *testing.T) {
	t.Run("on an instance that holds nothing else", func(t *testing.T) {
		h := newEngineHarness(t, "Cancel Orphan Alone Project")
		events := &eventLog{}
		h.dispatcher.Register(events)
		h.recordsAsProductionDoes()
		w := newWaiver(h)
		ctx := h.Ctx()
		id := w.start(t, nothingFollowsAStepReachedTwice(h.projID, "orphan-alone"), nil)
		orphan := finishOneOfTwo(t, h, id)
		if left := requireInstanceStatus(ctx, t, h, id, entities.ProcessActive); len(left.Tokens) != 0 {
			t.Fatalf("the instance still holds %d token(s); this test needs it to hold none", len(left.Tokens))
		}

		closing := deviationCommand(entities.DeviationCancel, id, "", nil)
		plan := w.preview(t, closing)
		wantWarnings := []string{
			"This instance is not waiting at any step. Cancelling it closes it.",
			"“Check the order” is still open though the instance is not waiting there; it will be withdrawn.",
			"“Check the order” is with rita, who will be told it was withdrawn.",
		}
		if !plan.Applicable() || !reflect.DeepEqual(plan.Warnings, wantWarnings) || len(plan.OpenWork) != 1 || plan.OpenWork[0].TaskID != orphan.ID {
			t.Fatalf("the plan: open %+v, refusals:%s\nwarnings:%s", plan.OpenWork, lines(plan.Refusals), lines(plan.Warnings))
		}
		w.mustApply(t, closing)

		requireInstanceStatus(ctx, t, h, id, entities.ProcessCancelled)
		if task, err := h.svc.GetTask(ctx, orphan.ID); err != nil || task.Status != entities.TaskCanceled {
			t.Fatalf("the task nobody waited for is %q (%v), want it withdrawn", task.Status, err)
		}
		if told := toldOfWithdrawal(events); !reflect.DeepEqual(told, map[string]int{"rita": 1}) {
			t.Fatalf("withdrawals were announced to %v, want rita told once", told)
		}
		if told := withdrawalNotices(t, h, "rita"); told != 1 {
			t.Errorf("rita has %d notice(s) that a task was withdrawn, want one", told)
		}
		rows := w.ledger(t, id)
		if len(rows) != 1 {
			t.Fatalf("the ledger holds %d rows, want one", len(rows))
		}
		row := rows[0]
		requireNoStep(t, row, theEntryOf(t, h, id, serviceimpl.EventInstanceCancelled))
		was := tasksSection(t, row.Before)
		if len(was) != 1 || was[orphan.ID.String()]["assignee"] != "rita" || row.Details["withdrawn"] != float64(1) ||
			statusIn(row.Before) != "active" || statusIn(row.After) != "cancelled" {
			t.Fatalf("the row: before %v, after %v, details %v", row.Before, row.After, row.Details)
		}
	})

	t.Run("on an instance that waits somewhere else", func(t *testing.T) {
		h := newEngineHarness(t, "Cancel Orphan Beside Project")
		events := &eventLog{}
		h.dispatcher.Register(events)
		h.recordsAsProductionDoes()
		w := newWaiver(h)
		ctx := h.Ctx()
		id := w.start(t, twoFlowsIntoOneStep(h.projID, "orphan-beside"), nil)
		orphan := finishOneOfTwo(t, h, id)
		packing := theOpenTask(t, h, id, "pack")

		w.mustApply(t, deviationCommand(entities.DeviationCancel, id, "pack", nil))
		requireInstanceStatus(ctx, t, h, id, entities.ProcessCancelled)
		for what, task := range map[string]entities.Task{"the task nobody waited for": orphan, "the task the instance waited at": packing} {
			if now, err := h.svc.GetTask(ctx, task.ID); err != nil || now.Status != entities.TaskCanceled {
				t.Errorf("%s is %q (%v), want it withdrawn", what, now.Status, err)
			}
		}
		if told := toldOfWithdrawal(events); !reflect.DeepEqual(told, map[string]int{"rita": 1, "paul": 1}) {
			t.Fatalf("withdrawals were announced to %v, want rita and paul told once each", told)
		}
		rows := w.ledger(t, id)
		if len(rows) != 1 {
			t.Fatalf("the ledger holds %d rows, want one", len(rows))
		}
		row := rows[0]
		was := tasksSection(t, row.Before)
		if row.Node == nil || row.Node.ID != "pack" || len(was) != 2 || row.Details["withdrawn"] != float64(2) || row.Task != nil ||
			was[orphan.ID.String()]["assignee"] != "rita" || was[packing.ID.String()]["assignee"] != "paul" {
			t.Fatalf("the row: step %+v, task %+v, before %v, details %v", row.Node, row.Task, row.Before, row.Details)
		}
	})
}

// Rulings addendum §10, for a called instance (plan Ruling 24). One that
// holds nothing will never end and never resume its caller, and while it is
// active its caller cannot be cancelled either. It is the one called instance
// that may be cancelled alone; its caller, which is not resumed, is then
// closed in its turn, at the step where it was waiting for it.
func TestAStrandedCalledInstanceAndItsCallerCanBothBeClosed(t *testing.T) {
	h := newEngineHarness(t, "Stranded Called Project")
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	ctx := h.Ctx()
	h.deploy(t, nothingFollows(h.projID, "order-check-called"))
	parent := w.start(t, callerOf(h.projID, "order-intake-stranded", "order-check-called"), nil)
	child := theOneCalledBy(t, h, parent)
	finishTheOnlyStep(t, h, child)

	around := deviationCommand(entities.DeviationCancel, parent, "haveItChecked", nil)
	if plan := w.preview(t, around); plan.Applicable() {
		t.Fatal("the caller was cancellable around a called instance that is still active")
	}
	if out, err := w.apply(t, around); !errors.Is(err, apierr.ErrInvalidArgument) || out.Applied {
		t.Fatalf("the caller, cancelled around a called instance that is still active: %+v, %v", out, err)
	}
	alone := deviationCommand(entities.DeviationCancel, child, "", nil)
	plan := w.preview(t, alone)
	if !plan.Applicable() || !strings.Contains(strings.Join(plan.Warnings, " | "), parent.String()) {
		t.Fatalf("closing the stranded called instance: refusals %q, warnings %q; want it accepted with a warning naming its caller",
			plan.Refusals, plan.Warnings)
	}
	w.mustApply(t, alone)
	requireInstanceStatus(ctx, t, h, child, entities.ProcessCancelled)
	childRows := w.ledger(t, child)
	if len(childRows) != 1 || childRows[0].Kind != entities.DeviationCancel {
		t.Fatalf("the called instance's ledger: %+v", childRows)
	}
	requireNoStep(t, childRows[0], theEntryOf(t, h, child, serviceimpl.EventInstanceCancelled))
	// The caller is not resumed: it waits where it waited, and nothing of it
	// is recorded as deviating.
	if waiting := requireInstanceStatus(ctx, t, h, parent, entities.ProcessActive); tokensOn(t, h, parent, "haveItChecked") != 1 || len(waiting.Tokens) != 1 {
		t.Fatalf("the caller holds %d token(s), %d on the call; closing what it called was not to move it", len(waiting.Tokens), tokensOn(t, h, parent, "haveItChecked"))
	}
	if rows := w.ledger(t, parent); len(rows) != 0 {
		t.Fatalf("closing the called instance wrote %d ledger row(s) on its caller", len(rows))
	}

	w.mustApply(t, around)
	requireInstanceStatus(ctx, t, h, parent, entities.ProcessCancelled)
	rows := w.ledger(t, parent)
	if len(rows) != 1 || rows[0].Node == nil || rows[0].Node.ID != "haveItChecked" || rows[0].Node.Name != "Have it checked" {
		t.Fatalf("the caller's ledger: %+v", rows)
	}
	entry := theEntryOf(t, h, parent, serviceimpl.EventInstanceCancelled)
	if want := "This instance was ended at “Have it checked” by ana. Reason: " + waiveReason + "."; entry.Narrative != want || entry.Data["node_id"] != "haveItChecked" {
		t.Errorf("the caller's entry reads %q with %+v\nwant\n  %s", entry.Narrative, entry.Data, want)
	}
}

// A cancel ends the whole instance, and what a plan lists of its work is the
// first two hundred tasks. The act takes every one: each task is withdrawn,
// each holder told once, and the record counts what was withdrawn, not what
// was listed.
//
// On the way: a task the plan does not list is closed, with no token moving,
// and the key the service answers changes — the key is of every open task,
// listed or not — so the apply of the earlier preview is refused.
func TestACancelOfMoreWorkThanAPlanListsWithdrawsAllOfIt(t *testing.T) {
	h := newEngineHarness(t, "Cancel Many Tasks Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	ctx := h.Ctx()
	const inAll = 205
	approvers := make([]any, inAll)
	for i := range approvers {
		approvers[i] = fmt.Sprintf("approver%03d", i)
	}
	id := startApproval(t, h, approvalDefinition(h.projID, "cancel-many-tasks", "parallel", ""), approvers...)
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

	cancel := deviationCommand(entities.DeviationCancel, id, "approve", nil)
	stale := w.previewed(t, cancel)
	first := w.preview(t, cancel)
	if len(first.OpenWork) != 200 || first.OpenWorkInAll != inAll {
		t.Fatalf("the plan lists %d of %d tasks; this test needs 200 of %d", len(first.OpenWork), first.OpenWorkInAll, inAll)
	}
	listed := map[uuid.UUID]bool{}
	for _, work := range first.OpenWork {
		listed[work.TaskID] = true
	}
	var unlisted []entities.Task
	for _, task := range open {
		if !listed[task.ID] {
			unlisted = append(unlisted, task)
		}
	}
	if len(unlisted) != inAll-200 {
		t.Fatalf("%d open tasks are not listed; this test needs %d", len(unlisted), inAll-200)
	}

	// One the plan does not list is closed where no token moves.
	closed := unlisted[0]
	if err := h.repo.Task().UpdateStatus(ctx, closed.ID, models.TaskCanceled); err != nil {
		t.Fatalf("close a task the plan did not list: %v", err)
	}
	if tokens := tokensOn(t, h, id, "approve"); tokens != inAll {
		t.Fatalf("the step holds %d token(s) after a task was closed under it; this test needs all %d still there", tokens, inAll)
	}
	second := w.preview(t, cancel)
	if second.VisitKey == first.VisitKey || second.OpenWorkInAll != inAll-1 || len(second.OpenWork) != 200 {
		t.Fatalf("after a task the plan did not list was closed: the key changed: %v, %d listed of %d; want a new key and 200 of %d",
			second.VisitKey != first.VisitKey, len(second.OpenWork), second.OpenWorkInAll, inAll-1)
	}
	closedOne := everyRow(t, h)
	if _, err := w.svc.DeviateInstance(w.ctx, stale); !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "preview again") {
		t.Fatalf("the cancel previewed before the task was closed: %v, want it told to preview again", err)
	}
	if changed := tablesThatDiffer(closedOne, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("the refused cancel changed %v", changed)
	}

	out := w.mustApply(t, cancel)
	const taken = inAll - 1
	requireInstanceStatus(ctx, t, h, id, entities.ProcessCancelled)
	if left := openIterationTasks(ctx, t, h, id, "approve"); len(left) != 0 {
		t.Fatalf("%d task(s) are still open on a cancelled instance", len(left))
	}
	if len(out.Plan.OpenWork) != 200 || out.Plan.OpenWorkInAll != taken {
		t.Fatalf("the plan the apply made lists %d of %d; this test rests on it listing fewer than were withdrawn", len(out.Plan.OpenWork), out.Plan.OpenWorkInAll)
	}
	told := toldOfWithdrawal(events)
	if len(told) != taken {
		t.Fatalf("%d holder(s) were told of a withdrawal, want each of %d", len(told), taken)
	}
	for _, task := range open {
		holder := holderOf[task.ID]
		want := 1
		if task.ID == closed.ID {
			want = 0
		}
		if told[holder] != want {
			t.Errorf("%s was told %d time(s), want %d", holder, told[holder], want)
		}
	}
	// Somebody whose task the plan listed, and somebody whose task it did not.
	for _, holder := range []string{holderOf[first.OpenWork[0].TaskID], holderOf[unlisted[1].ID]} {
		if notices := withdrawalNotices(t, h, holder); notices != 1 {
			t.Errorf("%s has %d notice(s) that a task was withdrawn, want one", holder, notices)
		}
	}

	rows := w.ledger(t, id)
	if len(rows) != 1 {
		t.Fatalf("the ledger holds %d rows, want one", len(rows))
	}
	row := rows[0]
	was, is := tasksSection(t, row.Before), tasksSection(t, row.After)
	if row.Details["withdrawn"] != float64(taken) || len(was) != taken || len(is) != taken || row.Task != nil {
		t.Fatalf("the row counts %v withdrawn and names %d before and %d after; want %d, the tasks it took", row.Details["withdrawn"], len(was), len(is), taken)
	}
	for _, task := range open {
		recorded, has := was[task.ID.String()]
		if task.ID == closed.ID {
			if has {
				t.Errorf("the row says the cancel withdrew a task that was closed before it")
			}
			continue
		}
		if !has || recorded["assignee"] != holderOf[task.ID] || recorded["status"] != string(entities.TaskClaimed) {
			t.Errorf("the row says of %s's task: %v", holderOf[task.ID], recorded)
		}
	}
}
