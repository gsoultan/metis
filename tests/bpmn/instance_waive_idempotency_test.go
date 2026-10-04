package bpmn_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/tests/testutils"
)

// previewed is a command made ready to apply: it names the plan a preview of
// it answered.
func (w waiver) previewed(t *testing.T, cmd entities.DeviationCommand) entities.DeviationCommand {
	t.Helper()
	cmd.VisitKey = w.preview(t, cmd).VisitKey
	cmd.DryRun = false
	return cmd
}

// sendApply applies a command on a goroutine of its own. The request gives up
// after lockWait, so one that waits for ever ends the test instead of hanging
// it.
func (w waiver) sendApply(cmd entities.DeviationCommand) *sent[entities.DeviationOutcome] {
	return send(func() (entities.DeviationOutcome, error) {
		inTime, stop := context.WithTimeout(w.ctx, lockWait)
		defer stop()
		return w.svc.DeviateInstance(inTime, cmd)
	})
}

// holdingTheTask holds a task's row and changes nothing on it, as a request
// that has taken the task and not finished does.
func (h engineHarness) holdingTheTask(t *testing.T, taskID uuid.UUID) *heldRows {
	t.Helper()
	return h.holding(t, `UPDATE tasks SET status = status WHERE id = ?`, taskID)
}

// skippedEntries is the node_skipped entries on an instance's trail.
func skippedEntries(t *testing.T, h engineHarness, id uuid.UUID) []entities.AuditEntry {
	t.Helper()
	entries, err := h.svc.GetAuditLogs(h.Ctx(), id)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	var skipped []entities.AuditEntry
	for _, entry := range entries {
		if entry.Type == serviceimpl.EventNodeSkipped {
			skipped = append(skipped, entry)
		}
	}
	return skipped
}

// rowAsAnswered is what a ledger row says, as a client is told it: its
// identity, who and why, and what it changed, written as JSON so that a row
// just made and the same row read back compare as the same answer.
func rowAsAnswered(t *testing.T, row entities.Deviation) string {
	t.Helper()
	said := map[string]any{
		"id": row.ID, "kind": row.Kind, "scope": row.Scope, "origin": row.Origin, "status": row.Status,
		"actor": row.Actor, "reason": row.Reason, "visit_key": row.VisitKey, "run_id": row.RunID,
		"audit_entry_id": row.AuditEntryID, "before": row.Before, "after": row.After, "details": row.Details,
	}
	if row.Node != nil {
		said["node"] = []string{row.Node.ID, row.Node.Name}
	}
	if row.Task != nil {
		said["task"] = row.Task.ID
	}
	encoded, err := json.Marshal(said)
	if err != nil {
		t.Fatalf("write the row down: %v", err)
	}
	return string(encoded)
}

// Review Focus 2. A double click, or a client retrying a response it lost:
// two applies of one preview at once. They take turns at the instance's lock;
// the second finds the first's ledger row for the same visit and replays it.
//
// The order is made, not hoped for: the first apply is stopped where it holds
// the instance and waits for the task's row, and the second is sent only then,
// so it is waiting for the instance while the first is in the middle of its
// waive.
func TestTwoAppliesOfOnePreviewWaiveOnce(t *testing.T) {
	h := newEngineHarness(t, "Waive Race Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-race"), nil)
	task := theOpenTask(t, h, id, "opsApprove")
	cmd := w.previewed(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))

	held := h.holdingTheTask(t, task.ID)
	first := w.sendApply(cmd)
	h.waitForWaiters(t, held, 1, first.answered)
	second := w.sendApply(cmd)
	h.waitForWaiters(t, held, 2, first.answered, second.answered)
	held.letGo(t, false)

	acted, err := first.answer(t, "the first apply")
	if err != nil {
		t.Fatalf("the first apply: %v", err)
	}
	replayed, err := second.answer(t, "the second apply")
	if err != nil {
		t.Fatalf("the second apply: %v", err)
	}
	if !acted.Applied || acted.Replayed || acted.Deviation == nil {
		t.Fatalf("the apply that got there first answered %+v, want it to have acted", acted)
	}
	if !replayed.Applied || !replayed.Replayed || replayed.Deviation == nil {
		t.Fatalf("the apply that waited answered %+v, want a replay", replayed)
	}
	if acted.Deviation.ID != replayed.Deviation.ID {
		t.Fatal("the replay answered a different row from the one the apply wrote")
	}
	if replayed.Plan.VisitKey != cmd.VisitKey || replayed.Plan.NodeID != "opsApprove" || replayed.Plan.Kind != entities.DeviationWaive {
		t.Errorf("the replay's plan %+v does not say what was replayed", replayed.Plan)
	}
	if rows := w.ledger(t, id); len(rows) != 1 {
		t.Fatalf("two applies of one preview wrote %d ledger rows", len(rows))
	}
	if withdrawn := events.ofType(entities.EventTaskCanceled); len(withdrawn) != 1 {
		t.Fatalf("the task was withdrawn %d times", len(withdrawn))
	}
	if entries := skippedEntries(t, h, id); len(entries) != 1 {
		t.Fatalf("the trail says the step was waived %d times", len(entries))
	}
	if seen := tasksEverOn(t, h, id, "salesApprove"); seen != 1 {
		t.Fatalf("the process moved on %d times, want once", seen)
	}
}

// The same, with nothing arranged: several applies of one preview let go
// together, over and over. Whatever order they land in, one acts and the rest
// answer its row.
func TestAppliesOfOnePreviewSentTogetherWaiveOnce(t *testing.T) {
	h := newEngineHarness(t, "Waive Free Race Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	w := newWaiver(h)
	h.deploy(t, opsApproval(h.projID, "ops-free-race"))
	const rounds, together = 6, 3
	for round := range rounds {
		id, err := h.svc.StartProcess(h.Ctx(), h.projID, "ops-free-race", nil)
		if err != nil {
			t.Fatalf("round %d: start: %v", round, err)
		}
		cmd := w.previewed(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))

		var start sync.WaitGroup
		start.Add(1)
		applies := make([]*sent[entities.DeviationOutcome], together)
		for i := range applies {
			applies[i] = send(func() (entities.DeviationOutcome, error) {
				start.Wait()
				inTime, stop := context.WithTimeout(w.ctx, lockWait)
				defer stop()
				return w.svc.DeviateInstance(inTime, cmd)
			})
		}
		start.Done()

		acted, row := 0, uuid.Nil
		for i, apply := range applies {
			out, err := apply.answer(t, "an apply")
			if err != nil {
				t.Fatalf("round %d, apply %d: %v", round, i, err)
			}
			if !out.Applied || out.Deviation == nil {
				t.Fatalf("round %d, apply %d answered %+v", round, i, out)
			}
			if !out.Replayed {
				acted++
			}
			if row != uuid.Nil && out.Deviation.ID != row {
				t.Fatalf("round %d: two applies of one preview answered different rows", round)
			}
			row = out.Deviation.ID
		}
		if acted != 1 {
			t.Fatalf("round %d: %d of %d applies acted, want exactly one", round, acted, together)
		}
		if rows := w.ledger(t, id); len(rows) != 1 {
			t.Fatalf("round %d: %d applies of one preview wrote %d ledger rows", round, together, len(rows))
		}
		if seen := tasksEverOn(t, h, id, "salesApprove"); seen != 1 {
			t.Fatalf("round %d: the process moved on %d times, want once", round, seen)
		}
		if withdrawn := events.ofType(entities.EventTaskCanceled); len(withdrawn) != round+1 {
			t.Fatalf("round %d: %d withdrawal(s) announced in all, want one for each round", round, len(withdrawn))
		}
	}
}

// A retry is the same request: the same act on the same step, for the same
// reason, counting as the same thing. Anything else for a visit already acted
// on is told who acted, and changes nothing.
func TestADifferentRequestForAVisitAlreadyActedOnIsRefused(t *testing.T) {
	h := newEngineHarness(t, "Waive Conflict Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-conflict"), nil)
	cmd := w.previewed(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", map[string]any{"approved": 1}))
	first, err := w.svc.DeviateInstance(w.ctx, cmd)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	after := everyRow(t, h)

	otherReason := cmd
	otherReason.Reason = "a different reason, sent later"
	otherValue := cmd
	otherValue.Outputs = map[string]any{"approved": 2}
	otherType := cmd
	otherType.Outputs = map[string]any{"approved": "1"}
	noValue := cmd
	noValue.Outputs = nil
	for what, different := range map[string]entities.DeviationCommand{
		"another reason": otherReason, "another value": otherValue, "the value as text": otherType, "no value": noValue,
	} {
		out, err := w.svc.DeviateInstance(w.ctx, different)
		if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "already waived by ana") {
			t.Errorf("%s for the same visit: got %v, want it refused as already waived by ana", what, err)
		}
		if out.Applied || out.Replayed || out.Deviation != nil {
			t.Errorf("%s for the same visit was answered as though it had been done: %+v", what, out)
		}
	}

	// The same request, as a client that lost the answer sends it again: the
	// reason with the spaces a form left around it, the number as another
	// decoder reads it.
	spaced := cmd
	spaced.Reason = "  " + cmd.Reason + "\n"
	asFloat := cmd
	asFloat.Outputs = map[string]any{"approved": 1.0}
	asNumber := cmd
	asNumber.Outputs = map[string]any{"approved": json.Number("1")}
	for what, same := range map[string]entities.DeviationCommand{
		"the same request": cmd, "the reason with spaces around it": spaced,
		"the number as a float": asFloat, "the number as a decoder kept it": asNumber,
	} {
		out, err := w.svc.DeviateInstance(w.ctx, same)
		if err != nil || !out.Applied || !out.Replayed || out.Deviation == nil || out.Deviation.ID != first.Deviation.ID {
			t.Errorf("%s, sent again: %+v, %v; want a replay of the row the apply wrote", what, out, err)
		}
	}
	if changed := tablesThatDiffer(after, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("requests for a visit already acted on changed %v", changed)
	}
	if rows := w.ledger(t, id); len(rows) != 1 {
		t.Fatalf("the ledger holds %d rows", len(rows))
	}
}

// A retry of an act already made answers its row, whatever the instance has
// become since: that is what makes a lost reply safe to send again. The
// instance has moved on, and then ended; the retry is still told what it did.
func TestARetryOfAWaiveAnswersItsRowWhateverTheInstanceHasBecome(t *testing.T) {
	h := newEngineHarness(t, "Waive Retry Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, opsApproval(h.projID, "ops-retry"), nil)
	cmd := w.previewed(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	first, err := w.svc.DeviateInstance(w.ctx, cmd)
	if err != nil || first.Replayed {
		t.Fatalf("apply: %+v, %v", first, err)
	}

	retry := func(when string) {
		t.Helper()
		before := everyRow(t, h)
		out, err := w.svc.DeviateInstance(w.ctx, cmd)
		if err != nil || !out.Applied || !out.Replayed || out.Deviation == nil {
			t.Fatalf("the retry %s: %+v, %v; want a replay", when, out, err)
		}
		if said, wrote := rowAsAnswered(t, *out.Deviation), rowAsAnswered(t, *first.Deviation); said != wrote {
			t.Errorf("the retry %s answered\n %s\nand the apply answered\n %s", when, said, wrote)
		}
		if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
			t.Fatalf("the retry %s changed %v", when, changed)
		}
	}
	retry("while the instance waits at the next step")
	completeTaskAt(ctx, t, h, id, "salesApprove", nil)
	requireInstanceStatus(ctx, t, h, id, entities.ProcessCompleted)
	retry("after the instance ended")

	// A request that is not the retry is told of the instance as it is now.
	other := cmd
	other.VisitKey = "dv1-not-the-key-of-any-preview-00000"
	if _, err := w.svc.DeviateInstance(w.ctx, other); !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "preview again") {
		t.Fatalf("another request after the instance ended: %v, want it told to preview again", err)
	}
}

// Review Focus 1. The holder completes the task between the administrator's
// preview and apply — here while the apply is on its way: the completion has
// the instance and is finishing, and the apply waits for it. When the apply's
// turn comes its plan is worked out again from the row it locked, the work is
// no longer the work that was previewed, and nothing is advanced a second
// time.
func TestAWaiveOfAStepThatMovedSinceThePreviewIsRefused(t *testing.T) {
	h := newEngineHarness(t, "Waive Stale Preview Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, opsApproval(h.projID, "ops-moved"), nil)
	task := theOpenTask(t, h, id, "opsApprove")
	cmd := w.previewed(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))

	// The completion takes the instance, then the task: stopped at the task,
	// it holds the instance.
	held := h.holdingTheTask(t, task.ID)
	completion := send(func() (struct{}, error) {
		inTime, stop := context.WithTimeout(testutils.AsOperator(ctx, "ollie"), lockWait)
		defer stop()
		return struct{}{}, h.svc.CompleteTask(inTime, task.ID, "ollie", map[string]any{"approved": true})
	})
	h.waitForWaiters(t, held, 1, completion.answered)
	apply := w.sendApply(cmd)
	h.waitForWaiters(t, held, 2, completion.answered, apply.answered)
	held.letGo(t, false)

	if _, err := completion.answer(t, "ollie's completion"); err != nil {
		t.Fatalf("ollie completes: %v", err)
	}
	out, err := apply.answer(t, "the apply")
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "this instance has moved since you previewed it; preview again") {
		t.Fatalf("an apply after the step moved: got %v, want it refused with preview again", err)
	}
	if out.Applied || out.Replayed || out.Deviation != nil {
		t.Errorf("the refused apply answered as though it had acted: %+v", out)
	}
	if seen := tasksEverOn(t, h, id, "salesApprove"); seen != 1 {
		t.Fatalf("the process moved past the operations approval %d times, want once", seen)
	}
	if tokens := tokensOn(t, h, id, "salesApprove"); tokens != 1 {
		t.Fatalf("the instance holds %d token(s) on the sales approval, want one", tokens)
	}
	if rows := w.ledger(t, id); len(rows) != 0 {
		t.Fatalf("a refused apply wrote %d ledger row(s)", len(rows))
	}
	if entries := skippedEntries(t, h, id); len(entries) != 0 {
		t.Fatalf("the trail says a step ollie performed was waived: %+v", entries)
	}
	if withdrawn := events.ofType(entities.EventTaskCanceled); len(withdrawn) != 0 {
		t.Fatalf("a refused apply withdrew %d task(s)", len(withdrawn))
	}
	// What ollie did stays done, and stays his.
	done, err := h.svc.GetTask(ctx, task.ID)
	if err != nil || done.Status != entities.TaskCompleted {
		t.Fatalf("ollie's task is %q (%v), want completed", done.Status, err)
	}
	if value := variablesOf(t, h, id)["approved"]; value != true {
		t.Errorf("the instance holds approved = %v, want what ollie gave", value)
	}
}

// The same, with nothing in flight: the completion is long done when the
// apply arrives with the key of the plan made before it.
func TestAWaiveAppliedAfterItsHolderFinishedTheStepIsRefused(t *testing.T) {
	h := newEngineHarness(t, "Waive Late Apply Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, opsApproval(h.projID, "ops-late"), nil)
	cmd := w.previewed(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	task := theOpenTask(t, h, id, "opsApprove")
	if err := h.svc.CompleteTask(testutils.AsOperator(ctx, "ollie"), task.ID, "ollie", map[string]any{"approved": true}); err != nil {
		t.Fatalf("ollie completes: %v", err)
	}
	before := everyRow(t, h)
	_, err := w.svc.DeviateInstance(w.ctx, cmd)
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "preview again") {
		t.Fatalf("an apply after the step moved: got %v, want it refused with preview again", err)
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("the refused apply changed %v", changed)
	}
}

// The other way round: the waive has the instance and is withdrawing the
// task when its holder's completion arrives. The completion waits for the
// instance, and then finds the task withdrawn — it is refused, the step is
// not performed after it was waived, and the instance moves on once.
func TestACompletionThatArrivesWhileItsStepIsBeingWaivedIsRefused(t *testing.T) {
	h := newEngineHarness(t, "Waive Wins Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, opsApproval(h.projID, "ops-waive-wins"), nil)
	task := theOpenTask(t, h, id, "opsApprove")
	cmd := w.previewed(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))

	// The waive takes the instance, then the task: stopped at the task, it
	// holds the instance.
	held := h.holdingTheTask(t, task.ID)
	apply := w.sendApply(cmd)
	h.waitForWaiters(t, held, 1, apply.answered)
	completion := send(func() (struct{}, error) {
		inTime, stop := context.WithTimeout(testutils.AsOperator(ctx, "ollie"), lockWait)
		defer stop()
		return struct{}{}, h.svc.CompleteTask(inTime, task.ID, "ollie", map[string]any{"approved": true})
	})
	h.waitForWaiters(t, held, 2, apply.answered, completion.answered)
	held.letGo(t, false)

	out, err := apply.answer(t, "the apply")
	if err != nil || !out.Applied || out.Replayed {
		t.Fatalf("the waive that got there first: %+v, %v", out, err)
	}
	// Told what happened to the task, as somebody whose task was withdrawn is:
	// not that the server failed, which would have their client try again.
	if _, err := completion.answer(t, "ollie's completion"); !errors.Is(err, apierr.ErrInvalidArgument) ||
		!strings.Contains(err.Error(), "this task was withdrawn") {
		t.Fatalf("ollie's completion of a task a waive had withdrawn: got %v, want it refused as withdrawn", err)
	}
	if completed := events.ofType(entities.EventTaskCompleted); len(completed) != 0 {
		t.Fatalf("%d completion(s) were announced for a step that was waived", len(completed))
	}
	withdrawn, err := h.svc.GetTask(ctx, task.ID)
	if err != nil || withdrawn.Status != entities.TaskCanceled {
		t.Fatalf("the waived task is %q (%v), want withdrawn", withdrawn.Status, err)
	}
	if seen := tasksEverOn(t, h, id, "salesApprove"); seen != 1 {
		t.Fatalf("the process moved past the operations approval %d times, want once", seen)
	}
	if tokens := tokensOn(t, h, id, "salesApprove"); tokens != 1 {
		t.Fatalf("the instance holds %d token(s) on the sales approval, want one", tokens)
	}
	if _, set := variablesOf(t, h, id)["approved"]; set {
		t.Error("the refused completion set approved on an instance whose step was waived")
	}
	if rows := w.ledger(t, id); len(rows) != 1 {
		t.Fatalf("the ledger holds %d rows, want the waive", len(rows))
	}
}

// The record names who held the work when it was taken, not who the preview
// showed. ollie's task is being handed to carol when the waive arrives: the
// waive waits for the task's row, and what it then records and announces is
// what the row says once it has it. The plan in its answer was made before
// that and still shows ollie, which is why the record is not made from it.
func TestAWaiveRecordsWhoHeldTheWorkWhenItWasTaken(t *testing.T) {
	h := newEngineHarness(t, "Waive Holder Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-holder"), nil)
	task := theOpenTask(t, h, id, "opsApprove")
	cmd := w.previewed(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))

	// The hand-over, written and not yet committed.
	handOver := h.holding(t, `UPDATE tasks SET assignee = 'carol' WHERE id = ?`, task.ID)
	apply := w.sendApply(cmd)
	h.waitForWaiters(t, handOver, 1, apply.answered)
	handOver.letGo(t, true)

	out, err := apply.answer(t, "the apply")
	if err != nil || !out.Applied {
		t.Fatalf("the waive, once the hand-over was let go: %+v, %v", out, err)
	}
	if len(out.Plan.OpenWork) != 1 || out.Plan.OpenWork[0].Assignee != "ollie" {
		t.Fatalf("the plan the apply made shows %+v; it was made before the hand-over landed, and this test rests on that", out.Plan.OpenWork)
	}
	row := w.theWaive(t, id)
	was := tasksSection(t, row.Before)
	if was[task.ID.String()]["assignee"] != "carol" {
		t.Errorf("the ledger says %v held the task when it was withdrawn; carol did", was[task.ID.String()]["assignee"])
	}
	told := events.ofType(entities.EventTaskCanceled)
	if len(told) != 1 || told[0].Assignee != "carol" {
		t.Errorf("the withdrawal was announced to %+v, want carol alone", told)
	}
}

func TestAnApplyWithoutThePreviewsVisitKeyIsRefused(t *testing.T) {
	h := newEngineHarness(t, "Waive No Key Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-nokey"), nil)
	before := everyRow(t, h)
	cmd := deviationCommand(entities.DeviationWaive, id, "opsApprove", nil)
	cmd.DryRun = false
	if _, err := w.svc.DeviateInstance(w.ctx, cmd); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("an apply with no visit key: %v", err)
	}
	// A key that is not the plan's is no better than none.
	cmd.VisitKey = "dv1-not-the-key-of-any-preview-00000"
	if _, err := w.svc.DeviateInstance(w.ctx, cmd); !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "preview again") {
		t.Fatalf("an apply with a key no preview gave: %v, want it told to preview again", err)
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("applies that named no plan changed %v", changed)
	}
}

// An apply is answered inside the organization the request is for, as a
// preview is: another organization's administrator is told there is no such
// instance — before the waive and after it, when there is a row a retry would
// be answered with — in the words used for one that never existed.
func TestAnApplyFromAnotherOrganizationFindsNoInstance(t *testing.T) {
	h := newEngineHarness(t, "Waive Tenant Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-tenant"), nil)
	elsewhere, err := h.svc.CreateOrganization(t.Context(), "Another Organization", "")
	if err != nil {
		t.Fatalf("create the other organization: %v", err)
	}
	outsider := signedInAs(
		entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: elsewhere.ID.String()}),
		entities.User{Username: "otto", RolesByOrganization: map[uuid.UUID][]string{elsewhere.ID: {entities.RoleAdmin}}})
	cmd := w.previewed(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	nowhere := cmd
	nowhere.InstanceID = uuid.Must(uuid.NewV7())

	refusedAsNoSuchInstance := func(when string) {
		t.Helper()
		before := everyRow(t, h)
		out, errTheirs := w.svc.DeviateInstance(outsider, cmd)
		_, errNone := w.svc.DeviateInstance(outsider, nowhere)
		if !errors.Is(errTheirs, apierr.ErrNotFound) || !errors.Is(errNone, apierr.ErrNotFound) {
			t.Fatalf("%s: got %v for the instance and %v for one that does not exist, want not found for both", when, errTheirs, errNone)
		}
		if errTheirs.Error() != errNone.Error() {
			t.Errorf("%s: told %q of this instance and %q of one that does not exist; the difference says it exists", when, errTheirs, errNone)
		}
		if out.Applied || out.Replayed || out.Deviation != nil {
			t.Errorf("%s: answered %+v", when, out)
		}
		if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
			t.Fatalf("%s: another organization's apply changed %v", when, changed)
		}
	}
	refusedAsNoSuchInstance("before the waive")
	if _, err := w.svc.DeviateInstance(w.ctx, cmd); err != nil {
		t.Fatalf("the instance's own administrator applies: %v", err)
	}
	refusedAsNoSuchInstance("after the waive, with the very request that made it")
}

// Review Focus 5, at the service, for the request that changes something: an
// apply — a real one, naming the key of a plan nothing refuses — by anybody
// but an administrator of the instance's organization. Each is refused, and
// every table is as it was; then the administrator sends the very same
// command and it is applied, so what was refused was the caller and not the
// request.
func TestAnApplyByAnybodyButTheOrganizationsAdministratorChangesNothing(t *testing.T) {
	h := newEngineHarness(t, "Waive Apply Authority Project")
	h.recordsAsProductionDoes()
	events := &eventLog{}
	h.dispatcher.Register(events)
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-apply-authority"), nil)
	here := entities.ActingOrganization(h.Ctx())
	elsewhere, err := h.svc.CreateOrganization(t.Context(), "Another Organization", "")
	if err != nil {
		t.Fatalf("create the other organization: %v", err)
	}
	inTheOther := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: elsewhere.ID.String()})
	apply := w.previewed(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", map[string]any{"approved": true}))
	raised := events.count()

	for who, caller := range map[string]struct {
		ctx  context.Context
		want error
	}{
		"nobody signed in":      {h.Ctx(), apierr.ErrForbidden},
		"a member":              {signedInAs(h.Ctx(), entities.User{Username: "mia", Roles: []string{entities.RoleUser}}), apierr.ErrForbidden},
		"an operator":           {testutils.AsOperator(h.Ctx(), "olga"), apierr.ErrForbidden},
		"a designer":            {signedInAs(h.Ctx(), entities.User{Username: "dina", Roles: []string{entities.RoleDesigner}}), apierr.ErrForbidden},
		"the task's own holder": {testutils.AsOperator(h.Ctx(), "ollie"), apierr.ErrForbidden},
		"an administrator of another organization, asking in this one": {signedInAs(h.Ctx(), entities.User{Username: "otto",
			RolesByOrganization: map[uuid.UUID][]string{elsewhere.ID: {entities.RoleAdmin}}}), apierr.ErrForbidden},
		"an administrator of another organization, asking in their own": {signedInAs(inTheOther, entities.User{Username: "otto",
			RolesByOrganization: map[uuid.UUID][]string{elsewhere.ID: {entities.RoleAdmin}}}), apierr.ErrNotFound},
		"an operator and designer of this organization alone": {signedInAs(h.Ctx(), entities.User{Username: "odile",
			RolesByOrganization: map[uuid.UUID][]string{here: {entities.RoleOperator, entities.RoleDesigner}}}), apierr.ErrForbidden},
	} {
		before := everyRow(t, h)
		out, err := w.svc.DeviateInstance(caller.ctx, apply)
		if !errors.Is(err, caller.want) {
			t.Errorf("%s applying: got %v, want %v", who, err, caller.want)
		}
		if out.Applied || out.Replayed || out.Deviation != nil {
			t.Errorf("%s applying was answered as though it had been done: %+v", who, out)
		}
		if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
			t.Fatalf("%s applying changed %v", who, changed)
		}
	}
	if now := events.count(); now != raised {
		t.Fatalf("refused applies raised %d event(s)", now-raised)
	}
	if open := theOpenTask(t, h, id, "opsApprove"); open.AssigneeUsername() != "ollie" {
		t.Fatalf("after the refused applies the task is with %q, want ollie still", open.AssigneeUsername())
	}

	out, err := w.svc.DeviateInstance(w.ctx, apply)
	if err != nil || !out.Applied || out.Replayed {
		t.Fatalf("the organization's administrator, sending the same command: %+v, %v; want it applied", out, err)
	}
}

// A cancel and a hold are planned and previewed, and not yet made in place:
// an apply of either is refused as it was before a waive could be applied,
// and changes nothing. This goes when they are made.
func TestACancelAndAHoldAreNotYetAppliedInPlace(t *testing.T) {
	h := newEngineHarness(t, "Waive Only Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-waive-only"), nil)
	before := everyRow(t, h)
	for _, kind := range []entities.DeviationKind{entities.DeviationCancel, entities.DeviationHold} {
		command := deviationCommand(kind, id, "opsApprove", nil)
		if plan := w.preview(t, command); !plan.Applicable() {
			t.Fatalf("a %s is refused by its plan, so this proves nothing:%s", kind, lines(plan.Refusals))
		}
		out, err := w.apply(t, command)
		if !errors.Is(err, apierr.ErrInvalidArgument) || out.Applied || out.Deviation != nil {
			t.Errorf("a %s applied: %+v, %v; want it refused", kind, out, err)
		}
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("a cancel and a hold that are not yet made changed %v", changed)
	}
}
