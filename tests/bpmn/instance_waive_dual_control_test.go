package bpmn_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/tests/testutils"
)

// D9: a waive is a control loosened on one instance, so it is asked for by one
// administrator and done only when a different one approves (B1, B3, B6).
func TestAWaiveWaitsForASecondAdministratorAndThenMovesOn(t *testing.T) {
	h := newEngineHarness(t, "Dual Control Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-dual"), nil)

	task := theOpenTask(t, h, id, "opsApprove")
	before, raised := everyRow(t, h), events.count()
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	if !asked.Plan.RequiresSecondApprover || asked.PendingApproval.RequestedBy != "ana" {
		t.Fatalf("the request: %+v", asked)
	}
	// Asking changes nothing of the instance or its work: the request, its
	// ledger row and its trail entry are all it writes, and nobody is told.
	if open := theOpenTask(t, h, id, "opsApprove"); open.ID != task.ID || open.AssigneeUsername() != "ollie" || events.count() != raised {
		t.Fatal("asking for a waive withdrew the task, took it from its holder, or told somebody of it")
	}
	if !h.waitingAt(h.Ctx(), t, id, "opsApprove") || tasksEverOn(t, h, id, "salesApprove") != 0 {
		t.Fatal("asking for a waive moved the instance")
	}
	wrote := tablesThatDiffer(before, everyRow(t, h))
	if len(wrote) != 3 || !strings.HasPrefix(wrote[0], "audit_logs ") || !strings.HasPrefix(wrote[1], "deviation_requests ") ||
		!strings.HasPrefix(wrote[2], "instance_deviations ") {
		t.Fatalf("asking for a waive changed %v, want the request, its row and its entry and nothing else", wrote)
	}
	rows := w.ledger(t, id)
	if len(rows) != 1 || rows[0].Status != entities.DeviationPendingApproval || rows[0].RequestID != asked.PendingApproval.RequestID {
		t.Fatalf("while it waits the ledger holds %+v", rows)
	}
	if asks := entriesOfType(t, h, id, serviceimpl.EventDeviationRequested); len(asks) != 1 {
		t.Fatalf("the trail says a waive was asked for %d times, want once", len(asks))
	}
	if skipped := entriesOfType(t, h, id, serviceimpl.EventNodeSkipped); len(skipped) != 0 {
		t.Fatalf("the trail says the step was waived while the request still waits: %+v", skipped)
	}

	approved, err := w.approvals.ApproveDeviationRequest(w.as("budi"), asked.PendingApproval.RequestID, "checked with finance")
	if err != nil {
		t.Fatalf("budi approves: %v", err)
	}
	if !approved.Applied || approved.Request.Status != entities.DeviationRequestApplied || approved.Request.DecidedBy != "budi" {
		t.Fatalf("the approval: %+v", approved)
	}
	if !h.waitingAt(h.Ctx(), t, id, "salesApprove") || len(events.ofType(entities.EventTaskCanceled)) != 1 {
		t.Fatal("the approved waive did not withdraw the task once and move on")
	}
	rows = w.ledger(t, id)
	if len(rows) != 1 || rows[0].ID != asked.Deviation.ID || rows[0].Status != entities.DeviationApplied ||
		rows[0].Actor != "ana" || rows[0].ApprovedBy != "budi" || rows[0].DecidedAt == nil {
		t.Fatalf("after approval the ledger holds %+v", rows)
	}
	skipped := theEntryOf(t, h, id, serviceimpl.EventNodeSkipped)
	if skipped.Data["request_id"] != asked.PendingApproval.RequestID.String() || skipped.Data["approved_by"] != "budi" {
		t.Fatalf("the node_skipped entry does not say whose request it was and who approved it: %+v", skipped.Data)
	}
	// Its sentence is the one a waive has always had, to the letter: it says
	// nobody performed the step, and "approved" in it would read as the
	// approval the step was for. Who approved the request is the entry beside it.
	if want := "“Operations approve” was waived — nobody performed it — by ana. Reason: " + waiveReason + "."; skipped.Narrative != want {
		t.Fatalf("the node_skipped entry reads\n  %s\nwant\n  %s", skipped.Narrative, want)
	}
	if _, said := skipped.Data["self_approved"]; said {
		t.Fatalf("an approval by a second administrator says something of a self-approval: %+v", skipped.Data)
	}
	if approvals := entriesOfType(t, h, id, serviceimpl.EventDeviationApproved); len(approvals) != 1 {
		t.Fatalf("the trail records %d approvals, want the one", len(approvals))
	}
}

// The record of an approved waive says who asked, who approved and when — on
// the request, on the ledger row and on the trail — by name and by account,
// and reads as a waive from end to end: nothing in it says the step's own
// work was approved.
func TestAnApprovedWaiveSaysWhoAskedWhoApprovedAndWhen(t *testing.T) {
	h := newEngineHarness(t, "Dual Control Record Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-record"), nil)
	open := theOpenTask(t, h, id, "opsApprove")
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	waiting := w.theWaive(t, id)
	if waiting.Actor != "ana" || waiting.ActorID != accountID("ana") || waiting.ApprovedBy != "" || waiting.DecidedAt != nil {
		t.Fatalf("the row of a waive that waits says somebody decided it: %+v", waiting)
	}

	from := time.Now().Add(-time.Second)
	approved, err := w.approvals.ApproveDeviationRequest(w.as("budi"), asked.PendingApproval.RequestID, "  checked with finance  ")
	if err != nil {
		t.Fatalf("budi approves: %v", err)
	}
	until := time.Now().Add(time.Second)
	within := func(at *time.Time) bool { return at != nil && at.After(from) && at.Before(until) }

	request := approved.Request
	if request.RequestedBy != "ana" || request.RequestedByID != accountID("ana") || request.DecidedBy != "budi" ||
		request.DecidedByID != accountID("budi") || request.DecisionReason != "checked with finance" || !within(request.DecidedAt) ||
		request.SelfApproved() {
		t.Fatalf("the request does not say who asked, who approved and when: %+v", request)
	}
	if stored, err := w.approvals.GetDeviationRequest(w.ctx, request.ID); err != nil || stored.Status != entities.DeviationRequestApplied ||
		stored.DecidedByID != accountID("budi") || stored.Outcome["deviation_id"] != asked.Deviation.ID.String() {
		t.Fatalf("the request as stored: %+v, %v", stored, err)
	}
	row := w.theWaive(t, id)
	if row.ID != asked.Deviation.ID || row.RunID != asked.Deviation.RunID || row.VisitKey != asked.Deviation.VisitKey || row.RequestID != request.ID {
		t.Fatalf("the approved row is not the row the request made: %+v, asked with %+v", row, asked.Deviation)
	}
	if row.Kind != entities.DeviationWaive || row.Status != entities.DeviationApplied || row.Actor != "ana" || row.ActorID != accountID("ana") ||
		row.ApprovedBy != "budi" || row.ApprovedByID != accountID("budi") || !within(row.DecidedAt) {
		t.Fatalf("the ledger row does not say who asked, who approved and when: %+v", row)
	}
	if _, said := row.Details["self_approved"]; said {
		t.Fatalf("a row a second administrator approved says something of a self-approval: %v", row.Details)
	}
	if approved.Deviation == nil || rowAsAnswered(t, *approved.Deviation) != rowAsAnswered(t, row) {
		t.Fatalf("the approval answered a row the ledger does not hold:\n  %+v\n  %+v", approved.Deviation, row)
	}

	// The row points at the entry that says the step was waived, and that
	// entry at the row; the approval is an entry of its own beside it.
	skipped := theEntryOf(t, h, id, serviceimpl.EventNodeSkipped)
	if skipped.ID != row.AuditEntryID || skipped.Data["deviation_id"] != row.ID.String() || skipped.Data["actor"] != "ana" ||
		skipped.Data["outcome"] != "waived" {
		t.Fatalf("the row and its node_skipped entry do not name each other, or the entry is not a waive's: %+v", skipped)
	}
	entry := theEntryOf(t, h, id, serviceimpl.EventDeviationApproved)
	if want := "budi approved ana's request to waive “Operations approve”. Note: checked with finance"; entry.Narrative != want {
		t.Fatalf("the approval reads\n  %s\nwant\n  %s", entry.Narrative, want)
	}
	if entry.Data["request_id"] != request.ID.String() || entry.Data["deviation_id"] != row.ID.String() ||
		entry.Data["requested_by"] != "ana" || entry.Data["approved_by"] != "budi" {
		t.Fatalf("the approval's entry does not name the request, the row, who asked and who approved: %+v", entry.Data)
	}
	if said := entriesOfType(t, h, id, serviceimpl.EventDeviationSelfApproved); len(said) != 0 {
		t.Fatalf("the trail says somebody approved their own request: %+v", said)
	}
	// Nothing says the step's work was done: the task that was open is
	// withdrawn, not completed.
	if task, err := h.svc.GetTask(h.Ctx(), open.ID); err != nil || task.Status != entities.TaskCanceled {
		t.Fatalf("the waived step's task is %q (%v), want withdrawn", task.Status, err)
	}
}

// The requester is identified by account id, so a rename does not make them
// somebody else (B3).
func TestTheRequesterNeverApprovesTheirOwnWaiveEvenRenamed(t *testing.T) {
	h := newEngineHarness(t, "Self Approval Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-self"), nil)
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	renamed := w.asAccount("ana-renamed", accountID("ana"))
	before := everyRow(t, h)
	for name, ctx := range map[string]context.Context{"ana": w.ctx, "ana renamed": renamed} {
		_, err := w.approvals.ApproveDeviationRequest(ctx, asked.PendingApproval.RequestID, "x")
		if !errors.Is(err, apierr.ErrForbidden) || !strings.Contains(err.Error(), "You asked for this") {
			t.Fatalf("%s approving her own request: %v, want forbidden, and told she asked for it", name, err)
		}
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("a refused self-approval changed %v", changed)
	}
	got, _ := w.approvals.GetDeviationRequest(w.ctx, asked.PendingApproval.RequestID)
	if got.Status != entities.DeviationRequestPending || len(openIterationTasks(h.Ctx(), t, h, id, "opsApprove")) != 1 {
		t.Fatalf("a refused self-approval changed something: %+v", got)
	}
	// Somebody else with her name is somebody else.
	if _, err := w.approvals.ApproveDeviationRequest(w.asAccount("ana", accountID("budi")), asked.PendingApproval.RequestID, ""); err != nil {
		t.Fatalf("another account that happens to be called ana: %v, want it approved", err)
	}
}

// Nobody but an administrator of the instance's organization, signed in with
// an account, decides a request — and whoever is refused changes nothing and
// learns nothing: an administrator of another organization is told there is
// no such request, as of one that never existed.
func TestOnlyAnAdministratorOfTheOrganizationApprovesAWaive(t *testing.T) {
	h := newEngineHarness(t, "Approval Authority Project")
	h.recordsAsProductionDoes()
	events := &eventLog{}
	h.dispatcher.Register(events)
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-approval-authority"), nil)
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	request := asked.PendingApproval.RequestID

	here := entities.ActingOrganization(h.Ctx())
	elsewhere, err := h.svc.CreateOrganization(t.Context(), "Another Organization", "")
	if err != nil {
		t.Fatalf("create the other organization: %v", err)
	}
	inTheOther := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: elsewhere.ID.String()})
	otto := entities.User{ID: accountID("otto"), Username: "otto", RolesByOrganization: map[uuid.UUID][]string{elsewhere.ID: {entities.RoleAdmin}}}
	callers := map[string]struct {
		ctx  context.Context
		want error
	}{
		"nobody signed in": {h.Ctx(), apierr.ErrForbidden},
		"a member":         {signedInAs(h.Ctx(), entities.User{ID: accountID("mia"), Username: "mia", Roles: []string{entities.RoleUser}}), apierr.ErrForbidden},
		"an operator":      {signedInAs(h.Ctx(), entities.User{ID: accountID("olga"), Username: "olga", Roles: []string{entities.RoleOperator}}), apierr.ErrForbidden},
		"the task's own holder": {signedInAs(h.Ctx(), entities.User{ID: accountID("ollie"), Username: "ollie", Roles: []string{entities.RoleOperator}}),
			apierr.ErrForbidden},
		"an operator and designer of this organization alone": {signedInAs(h.Ctx(), entities.User{ID: accountID("odile"), Username: "odile",
			RolesByOrganization: map[uuid.UUID][]string{here: {entities.RoleOperator, entities.RoleDesigner}}}), apierr.ErrForbidden},
		"an administrator of another organization, asking in this one":  {signedInAs(h.Ctx(), otto), apierr.ErrForbidden},
		"an administrator of another organization, asking in their own": {signedInAs(inTheOther, otto), apierr.ErrNotFound},
		"an administrator with no account": {signedInAs(h.Ctx(), entities.User{Username: "budi", Roles: []string{entities.RoleAdmin}}),
			apierr.ErrForbidden},
		"an administrator asking for no organization": {signedInAs(t.Context(), entities.User{ID: accountID("budi"), Username: "budi",
			Roles: []string{entities.RoleAdmin}}), apierr.ErrForbidden},
		"the requester": {w.ctx, apierr.ErrForbidden},
	}
	before, raised := everyRow(t, h), events.count()
	for who, caller := range callers {
		out, err := w.approvals.ApproveDeviationRequest(caller.ctx, request, "let it through")
		if !errors.Is(err, caller.want) {
			t.Errorf("%s approving: got %v, want %v", who, err, caller.want)
		}
		if out.Applied || out.Deviation != nil || out.WaivePlan != nil {
			t.Errorf("%s approving was answered as though it had been done: %+v", who, out)
		}
		// Nor may they read it: the request shows what was asked for and why.
		if who != "the requester" {
			if read, err := w.approvals.GetDeviationRequest(caller.ctx, request); !errors.Is(err, caller.want) || read.ID != uuid.Nil {
				t.Errorf("%s reading the request: got %+v, %v, want %v and nothing of it", who, read, err, caller.want)
			}
			listed, total, err := w.approvals.ListDeviationRequests(caller.ctx, entities.DeviationRequestQuery{})
			// Forbidden to whoever is forbidden; an administrator of another
			// organization lists their own, which holds none of this one's.
			if denied := errors.Is(err, apierr.ErrForbidden); len(listed) != 0 || total != 0 || denied != errors.Is(caller.want, apierr.ErrForbidden) {
				t.Errorf("%s listing the requests: %d of %d, %v", who, len(listed), total, err)
			}
		}
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("the refused approvals changed %v", changed)
	}
	if now := events.count(); now != raised {
		t.Fatalf("the refused approvals raised %d event(s)", now-raised)
	}
	if open := theOpenTask(t, h, id, "opsApprove"); open.AssigneeUsername() != "ollie" {
		t.Fatalf("after the refused approvals the task is with %q, want ollie still", open.AssigneeUsername())
	}

	// An administrator who holds the role in this organization alone is one.
	citra := entities.User{ID: accountID("citra"), Username: "citra", RolesByOrganization: map[uuid.UUID][]string{here: {entities.RoleAdmin}}}
	if out, err := w.approvals.ApproveDeviationRequest(signedInAs(h.Ctx(), citra), request, ""); err != nil || !out.Applied {
		t.Fatalf("a second administrator of this organization alone: %+v, %v; want it approved", out, err)
	}
}

// B4: the plan is made again at approval. The holder did the work meanwhile,
// so there is nothing to waive and the request is stale.
func TestAWaiveRequestGoesStaleWhenTheHolderCompletesTheStep(t *testing.T) {
	h := newEngineHarness(t, "Stale Waive Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-stale"), nil)
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	open := openIterationTasks(h.Ctx(), t, h, id, "opsApprove")
	if err := h.svc.CompleteTask(testutils.AsOperator(h.Ctx(), "ollie"), open[0].ID, "ollie", map[string]any{"approved": true}); err != nil {
		t.Fatalf("ollie completes: %v", err)
	}
	_, err := w.approvals.ApproveDeviationRequest(w.as("budi"), asked.PendingApproval.RequestID, "")
	want := apierr.Invalidf("This request no longer holds — the instance has moved since it was asked for — so nothing was applied. Preview again and ask afresh.")
	if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != want.Error() {
		t.Fatalf("approving a request whose step was done: %v\nwant it refused as stale, saying exactly\n  %v", err, want)
	}
	got, _ := w.approvals.GetDeviationRequest(w.ctx, asked.PendingApproval.RequestID)
	rows := w.ledger(t, id)
	if got.Status != entities.DeviationRequestStale || rows[0].Status != entities.DeviationStale || tasksEverOn(t, h, id, "salesApprove") != 1 {
		t.Fatalf("request %q, row %q, sales tasks %d", got.Status, rows[0].Status, tasksEverOn(t, h, id, "salesApprove"))
	}
	// Nobody approved it: the row and the request name who asked and nobody else.
	if rows[0].ApprovedBy != "" || rows[0].Task != nil || got.DecidedBy != "" || got.DecidedAt == nil {
		t.Fatalf("a stale request reads as somebody's decision: row %+v, request %+v", rows[0], got)
	}
	stale := entriesOfType(t, h, id, serviceimpl.EventDeviationStale)
	if len(stale) != 1 {
		t.Fatalf("the trail says the request went stale %d times, want once", len(stale))
	}
	if want := "The request to waive “Operations approve” that ana made no longer held when budi tried to approve it, " +
		"so nothing changed: the instance has moved since it was asked for"; stale[0].Narrative != want {
		t.Fatalf("the stale entry reads\n  %s\nwant\n  %s", stale[0].Narrative, want)
	}
	if skipped := entriesOfType(t, h, id, serviceimpl.EventNodeSkipped); len(skipped) != 0 {
		t.Fatalf("the trail says a step ollie performed was waived: %+v", skipped)
	}
	// It is over: approving it again is told what became of it, and the step
	// the instance now waits at can be asked for.
	if _, err := w.approvals.ApproveDeviationRequest(w.as("budi"), asked.PendingApproval.RequestID, ""); !errors.Is(err, apierr.ErrInvalidArgument) ||
		!strings.Contains(err.Error(), "This request went stale on ") {
		t.Fatalf("approving a stale request again: %v", err)
	}
	w.mustApply(t, deviationCommand(entities.DeviationWaive, id, "salesApprove", nil))
	requireInstanceStatus(h.Ctx(), t, h, id, entities.ProcessCompleted)
}

// Review Focus 4: the instance changed version, or was cancelled, while the
// waive waited.
func TestAWaiveRequestGoesStaleWhenTheInstanceIsMigratedOrCancelled(t *testing.T) {
	for how, why := range map[string]string{
		"migrated":  "the instance has moved since it was asked for",
		"cancelled": "the instance is cancelled",
	} {
		t.Run(how, func(t *testing.T) {
			h := newEngineHarness(t, "Moved Waive Project "+how)
			h.recordsAsProductionDoes()
			w := newWaiver(h)
			v1, err := h.svc.CreateDefinition(h.Ctx(), opsApproval(h.projID, "ops-moved-"+how))
			if err != nil {
				t.Fatalf("deploy v1: %v", err)
			}
			id, err := h.svc.StartProcess(h.Ctx(), h.projID, "ops-moved-"+how, nil)
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
			if how == "migrated" {
				v2, err := h.svc.CreateDefinition(h.Ctx(), opsApproval(h.projID, "ops-moved-"+how))
				if err != nil {
					t.Fatalf("deploy v2: %v", err)
				}
				if err := h.svc.MigrateInstances(h.Ctx(), v1, v2, nil); err != nil {
					t.Fatalf("a mapping-only migration: %v", err)
				}
			} else {
				w.mustApply(t, deviationCommand(entities.DeviationCancel, id, "opsApprove", nil))
			}
			before := everyRow(t, h)
			want := apierr.Invalidf("This request no longer holds — %s — so nothing was applied. Preview again and ask afresh.", why)
			if _, err := w.approvals.ApproveDeviationRequest(w.as("budi"), asked.PendingApproval.RequestID, ""); !errors.Is(err, apierr.ErrInvalidArgument) ||
				err.Error() != want.Error() {
				t.Fatalf("approving after the instance was %s: %v\nwant it refused, saying exactly\n  %v", how, err, want)
			}
			// The refusal records that the request went stale, and touches
			// nothing of the instance or its work.
			changed := tablesThatDiffer(before, everyRow(t, h))
			if len(changed) != 3 {
				t.Fatalf("a stale approval changed %v, want the request, its row and an entry", changed)
			}
			for _, table := range changed {
				if !strings.HasPrefix(table, "deviation_requests ") && !strings.HasPrefix(table, "instance_deviations ") && !strings.HasPrefix(table, "audit_logs ") {
					t.Fatalf("a stale approval changed %s", table)
				}
			}
			if got, _ := w.approvals.GetDeviationRequest(w.ctx, asked.PendingApproval.RequestID); got.Status != entities.DeviationRequestStale {
				t.Fatalf("the request reads %q", got.Status)
			}
			if row := w.theWaive(t, id); row.Status != entities.DeviationStale {
				t.Fatalf("the row of the stale request reads %q", row.Status)
			}
		})
	}
}

// A suspended instance has not ended and has not moved. The approval is
// refused as an apply on it is, in the same words, and the request is left
// waiting: it is not stale, for nothing it was asked for has changed. (Nothing
// in the product suspends an instance; the row is written as 3a-2's tests
// write it.)
func TestAWaiveRequestForASuspendedInstanceWaits(t *testing.T) {
	h := newEngineHarness(t, "Suspended Waive Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-suspended"), nil)
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	suspend(t, h, id)
	before := everyRow(t, h)
	_, err := w.approvals.ApproveDeviationRequest(w.as("budi"), asked.PendingApproval.RequestID, "")
	want := apierr.Invalidf("this instance is suspended, and a suspended instance is not waived, cancelled or held in place")
	if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != want.Error() {
		t.Fatalf("approving a waive of a suspended instance: %v\nwant exactly what an apply on it is told\n  %v", err, want)
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("the refused approval changed %v; the request must still be waiting", changed)
	}
	if got, _ := w.approvals.GetDeviationRequest(w.ctx, asked.PendingApproval.RequestID); got.Status != entities.DeviationRequestPending {
		t.Fatalf("the request reads %q, want it still pending", got.Status)
	}
}

// A control that is waived is said to be one from the first word to the last
// record: the plan the second administrator reads warns of it, as the
// requester's preview did, and the approved row and its entry are marked.
func TestAWaiveOfAControlWaitsWithItsWarningAndIsApprovedWithItsMark(t *testing.T) {
	h := newEngineHarness(t, "Control Waive Project")
	w := newWaiver(h)
	control := opsApproval(h.projID, "ops-dual-control")
	control.Nodes[1].Properties["compliance_relevant"] = true
	id := w.start(t, control, nil)
	const warning = "“Operations approve” is marked as a control. Waiving it is recorded as a control that was not performed."
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	if !said(asked.Plan.Warnings, warning) {
		t.Fatalf("the plan asked with does not warn that the step is a control:%s", lines(asked.Plan.Warnings))
	}
	waiting, err := w.approvals.GetDeviationRequest(w.as("budi"), asked.PendingApproval.RequestID)
	if err != nil {
		t.Fatalf("budi reads the request: %v", err)
	}
	if shown, _ := json.Marshal(waiting.Plan["warnings"]); !strings.Contains(string(shown), "is marked as a control") {
		t.Fatalf("what the second administrator reads does not warn that the step is a control: %s", shown)
	}
	if rows := w.ledger(t, id); len(rows) != 1 || rows[0].Task != nil || len(rows[0].Before) != 0 {
		t.Fatalf("the row of a waive that waits names work nobody has withdrawn: %+v", rows)
	}
	if _, err := w.approve(asked); err != nil {
		t.Fatalf("budi approves: %v", err)
	}
	row, entry := w.theWaive(t, id), theEntryOf(t, h, id, serviceimpl.EventNodeSkipped)
	if row.Details["control"] != true || entry.Data["control"] != true {
		t.Fatalf("the approved waive of a control: the row's details %v and the entry's data %v; want each to carry control: true", row.Details, entry.Data)
	}
	if row.Task == nil || row.Details["withdrawn"] != float64(1) || row.Details["tasks_listed"] != float64(1) {
		t.Fatalf("the approved row does not name and count the task it withdrew: task %+v, details %v", row.Task, row.Details)
	}
}

// B5: past its deadline nobody can approve it, and the refusal records it.
func TestAnExpiredWaiveRequestCannotBeApproved(t *testing.T) {
	h := newEngineHarness(t, "Expired Waive Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-expired"), nil)
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	if err := h.db.Exec(`UPDATE deviation_requests SET expires_at = now() - interval '1 minute' WHERE id = ?`, asked.PendingApproval.RequestID).Error; err != nil {
		t.Fatalf("let time pass: %v", err)
	}
	// The requester is refused as the requester, and records nothing: who may
	// decide is asked before the clock is.
	before := everyRow(t, h)
	if _, err := w.approvals.ApproveDeviationRequest(w.ctx, asked.PendingApproval.RequestID, "x"); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("the requester approving her own expired request: %v, want forbidden", err)
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("the requester's refused approval changed %v", changed)
	}
	_, err := w.approvals.ApproveDeviationRequest(w.as("budi"), asked.PendingApproval.RequestID, "")
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "expired") ||
		!strings.Contains(err.Error(), "nothing was applied. Ask again if it is still needed.") {
		t.Fatalf("approving an expired request: %v", err)
	}
	got, _ := w.approvals.GetDeviationRequest(w.ctx, asked.PendingApproval.RequestID)
	if got.Status != entities.DeviationRequestExpired || w.ledger(t, id)[0].Status != entities.DeviationExpired ||
		len(openIterationTasks(h.Ctx(), t, h, id, "opsApprove")) != 1 {
		t.Fatalf("after a refused late approval: request %q", got.Status)
	}
	// Written down, not only read off the clock; and once.
	var stored string
	if err := h.db.Raw(`SELECT status FROM deviation_requests WHERE id = ?`, asked.PendingApproval.RequestID).Scan(&stored).Error; err != nil ||
		stored != string(entities.DeviationRequestExpired) {
		t.Fatalf("the request is stored as %q (%v), want expired", stored, err)
	}
	if expired := entriesOfType(t, h, id, serviceimpl.EventDeviationExpired); len(expired) != 1 {
		t.Fatalf("the trail says the request expired %d times, want once", len(expired))
	}
	if _, err := w.approvals.ApproveDeviationRequest(w.as("budi"), asked.PendingApproval.RequestID, ""); !errors.Is(err, apierr.ErrInvalidArgument) ||
		!strings.Contains(err.Error(), "This request expired on ") {
		t.Fatalf("approving it again: %v", err)
	}
	if expired := entriesOfType(t, h, id, serviceimpl.EventDeviationExpired); len(expired) != 1 {
		t.Fatalf("a second late approval recorded the expiry again: %d entries", len(expired))
	}
	// The visit is free: the same waive is asked for afresh and approved.
	w.mustApply(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	if !h.waitingAt(h.Ctx(), t, id, "salesApprove") {
		t.Fatal("a waive asked for again after its request expired did not move the instance on")
	}
}

// Review Focus 3: the deadline is fixed when the request is made.
func TestTheApprovalWindowIsFixedWhenTheRequestIsMade(t *testing.T) {
	t.Setenv(serviceimpl.EnvDeviationApprovalTTL, "96h")
	h := newEngineHarness(t, "Window Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-window"), nil)
	before := time.Now()
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	deadline := asked.PendingApproval.ExpiresAt
	if deadline.Before(before.Add(96*time.Hour).Truncate(time.Microsecond)) || deadline.After(time.Now().Add(96*time.Hour)) {
		t.Fatalf("the deadline %v is not 96h after the request", deadline)
	}
	t.Setenv(serviceimpl.EnvDeviationApprovalTTL, "2h")
	got, _ := w.approvals.GetDeviationRequest(w.ctx, asked.PendingApproval.RequestID)
	if !got.ExpiresAt.Equal(deadline) {
		t.Fatalf("changing the setting moved a waiting request's deadline from %v to %v", deadline, got.ExpiresAt)
	}
}

// B7: two administrators approve at once. Both lock the request; the second
// finds it decided. One withdrawal, one row.
func TestTwoApprovalsAtOnceWaiveOnce(t *testing.T) {
	h := newEngineHarness(t, "Approval Race Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-approval-race"), nil)
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, name := range []string{"budi", "citra"} {
		wg.Go(func() {
			inTime, stop := context.WithTimeout(w.as(name), lockWait)
			defer stop()
			_, errs[i] = w.approvals.ApproveDeviationRequest(inTime, asked.PendingApproval.RequestID, "")
		})
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("both or neither approval succeeded: %v / %v", errs[0], errs[1])
	}
	for _, err := range errs {
		if err != nil && (!errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "approved this on")) {
			t.Fatalf("the losing approval answered %v", err)
		}
	}
	if len(events.ofType(entities.EventTaskCanceled)) != 1 || tasksEverOn(t, h, id, "salesApprove") != 1 || len(w.ledger(t, id)) != 1 {
		t.Fatal("two approvals acted twice")
	}
}

// The same, with the order made and not hoped for: the first approval is
// stopped where it holds the request and the instance and waits for the
// task's row, and the second is sent only then, so it waits for the request
// while the first is in the middle of its waive. It is then told who approved
// and that the waive was made — in words, not as the server's failure — and
// what was done was done once and is recorded once.
func TestAnApprovalBehindAnotherIsToldWhoApprovedAndActsOnNothing(t *testing.T) {
	h := newEngineHarness(t, "Approval Order Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-approval-order"), nil)
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	request := asked.PendingApproval.RequestID
	task := theOpenTask(t, h, id, "opsApprove")

	held := h.holdingTheTask(t, task.ID)
	first := w.sendApproval("budi", request)
	h.waitForWaiters(t, held, 1, first.answered)
	second := w.sendApproval("citra", request)
	h.waitForWaiters(t, held, 2, first.answered, second.answered)
	held.letGo(t, false)

	acted, err := first.answer(t, "budi's approval")
	if err != nil || !acted.Applied || acted.Request.DecidedBy != "budi" {
		t.Fatalf("the approval that got there first: %+v, %v", acted, err)
	}
	late, err := second.answer(t, "citra's approval")
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "budi approved this on ") ||
		!strings.HasSuffix(err.Error(), ", and it was applied.") {
		t.Fatalf("the approval that waited: %v, want it told that budi approved it and that it was applied", err)
	}
	if late.Applied || late.Deviation != nil || late.WaivePlan != nil {
		t.Errorf("the approval that waited answered as though it had acted: %+v", late)
	}
	if withdrawn := events.ofType(entities.EventTaskCanceled); len(withdrawn) != 1 {
		t.Fatalf("the task was withdrawn %d times", len(withdrawn))
	}
	if seen := tasksEverOn(t, h, id, "salesApprove"); seen != 1 {
		t.Fatalf("the process moved on %d times, want once", seen)
	}
	row := w.theWaive(t, id)
	if row.Status != entities.DeviationApplied || row.ApprovedBy != "budi" || row.ApprovedByID != accountID("budi") {
		t.Fatalf("the row says %+v, want it applied on budi's approval", row)
	}
	for eventType, want := range map[string]int{serviceimpl.EventNodeSkipped: 1, serviceimpl.EventDeviationApproved: 1, serviceimpl.EventDeviationRequested: 1} {
		if entries := entriesOfType(t, h, id, eventType); len(entries) != want {
			t.Fatalf("the trail has %d %s entries, want %d", len(entries), eventType, want)
		}
	}
	if got, _ := w.approvals.GetDeviationRequest(w.ctx, request); got.Status != entities.DeviationRequestApplied || got.DecidedByID != accountID("budi") {
		t.Fatalf("the request reads %+v, want it applied on budi's approval", got)
	}
}

// B7: the approval and the holder's completion each lock the instance; the
// first wins. Either the approval applies and the completion is refused, or
// the completion lands and the approval finds the request stale.
func TestApprovingWhileTheHolderCompletesEndsInExactlyOneOutcome(t *testing.T) {
	h := newEngineHarness(t, "Approve Complete Race Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-approve-complete"), nil)
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	task := openIterationTasks(h.Ctx(), t, h, id, "opsApprove")[0]
	var wg sync.WaitGroup
	var approveErr, completeErr error
	wg.Go(func() {
		inTime, stop := context.WithTimeout(w.as("budi"), lockWait)
		defer stop()
		_, approveErr = w.approvals.ApproveDeviationRequest(inTime, asked.PendingApproval.RequestID, "")
	})
	wg.Go(func() {
		inTime, stop := context.WithTimeout(testutils.AsOperator(h.Ctx(), "ollie"), lockWait)
		defer stop()
		completeErr = h.svc.CompleteTask(inTime, task.ID, "ollie", map[string]any{"approved": true})
	})
	wg.Wait()
	if (approveErr == nil) == (completeErr == nil) {
		t.Fatalf("approve %v, complete %v: want exactly one to win", approveErr, completeErr)
	}
	got, _ := w.approvals.GetDeviationRequest(w.ctx, asked.PendingApproval.RequestID)
	want := entities.DeviationRequestApplied
	if approveErr != nil {
		want = entities.DeviationRequestStale
	}
	if got.Status != want || tasksEverOn(t, h, id, "salesApprove") != 1 {
		t.Fatalf("request %q (want %q), sales tasks %d", got.Status, want, tasksEverOn(t, h, id, "salesApprove"))
	}
}

// The same race, each way round, with the order made: whichever of the
// approval and the completion has the instance finishes, and the other finds
// what it left. Each round is a test of its own.
func TestAnApprovalAndACompletionTakeTurnsAtTheInstance(t *testing.T) {
	type round struct {
		h        engineHarness
		w        waiver
		events   *eventLog
		id       uuid.UUID
		task     entities.Task
		request  uuid.UUID
		held     *heldRows
		complete func() *sent[struct{}]
	}
	start := func(t *testing.T, key string) round {
		h := newEngineHarness(t, "Approve Complete Order Project "+key)
		r := round{h: h, w: newWaiver(h), events: &eventLog{}}
		h.dispatcher.Register(r.events)
		id := r.w.start(t, opsApproval(h.projID, key), nil)
		asked := r.w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
		r.id, r.request, r.task = id, asked.PendingApproval.RequestID, theOpenTask(t, h, id, "opsApprove")
		r.held = h.holdingTheTask(t, r.task.ID)
		r.complete = func() *sent[struct{}] {
			return send(func() (struct{}, error) {
				inTime, stop := context.WithTimeout(testutils.AsOperator(h.Ctx(), "ollie"), lockWait)
				defer stop()
				return struct{}{}, h.svc.CompleteTask(inTime, r.task.ID, "ollie", map[string]any{"approved": true})
			})
		}
		return r
	}

	// The approval holds the request and the instance and is withdrawing the
	// task when the completion arrives: the completion waits for the instance
	// and then finds its task withdrawn.
	t.Run("the approval first", func(t *testing.T) {
		r := start(t, "ops-approval-first")
		approval := r.w.sendApproval("budi", r.request)
		r.h.waitForWaiters(t, r.held, 1, approval.answered)
		completion := r.complete()
		r.h.waitForWaiters(t, r.held, 2, approval.answered, completion.answered)
		r.held.letGo(t, false)

		if out, err := approval.answer(t, "the approval"); err != nil || !out.Applied {
			t.Fatalf("the approval that got there first: %+v, %v", out, err)
		}
		if _, err := completion.answer(t, "ollie's completion"); !errors.Is(err, apierr.ErrInvalidArgument) ||
			!strings.Contains(err.Error(), "this task was withdrawn") {
			t.Fatalf("ollie's completion of a task an approved waive had withdrawn: got %v, want it refused as withdrawn", err)
		}
		if got, _ := r.w.approvals.GetDeviationRequest(r.w.ctx, r.request); got.Status != entities.DeviationRequestApplied {
			t.Fatalf("the request reads %q, want applied", got.Status)
		}
		if completed := r.events.ofType(entities.EventTaskCompleted); len(completed) != 0 {
			t.Fatalf("%d completion(s) were announced for a step that was waived", len(completed))
		}
		if _, set := variablesOf(t, r.h, r.id)["approved"]; set {
			t.Error("the refused completion set approved on an instance whose step was waived")
		}
		if row := r.w.theWaive(t, r.id); row.Status != entities.DeviationApplied || tasksEverOn(t, r.h, r.id, "salesApprove") != 1 {
			t.Fatalf("the row reads %q and the process moved on %d times", row.Status, tasksEverOn(t, r.h, r.id, "salesApprove"))
		}
	})

	// The completion has the instance and is finishing the task when the
	// approval arrives: the approval holds the request and waits for the
	// instance. Its guards are asked of the row it then locks — the step has
	// been done — so the request goes stale and nothing is waived.
	t.Run("the completion first", func(t *testing.T) {
		r := start(t, "ops-completion-first")
		completion := r.complete()
		r.h.waitForWaiters(t, r.held, 1, completion.answered)
		approval := r.w.sendApproval("budi", r.request)
		r.h.waitForWaiters(t, r.held, 2, completion.answered, approval.answered)
		r.held.letGo(t, false)

		if _, err := completion.answer(t, "ollie's completion"); err != nil {
			t.Fatalf("ollie completes: %v", err)
		}
		out, err := approval.answer(t, "the approval")
		if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "This request no longer holds") ||
			!strings.Contains(err.Error(), "Preview again and ask afresh.") {
			t.Fatalf("an approval after the step was done: %v, want it refused as stale", err)
		}
		if out.Applied || out.Deviation != nil {
			t.Errorf("the refused approval answered as though it had acted: %+v", out)
		}
		if got, _ := r.w.approvals.GetDeviationRequest(r.w.ctx, r.request); got.Status != entities.DeviationRequestStale {
			t.Fatalf("the request reads %q, want stale", got.Status)
		}
		if row := r.w.theWaive(t, r.id); row.Status != entities.DeviationStale || row.ApprovedBy != "" {
			t.Fatalf("the row reads %+v, want it stale and approved by nobody", row)
		}
		if withdrawn := r.events.ofType(entities.EventTaskCanceled); len(withdrawn) != 0 {
			t.Fatalf("a stale approval withdrew %d task(s)", len(withdrawn))
		}
		if skipped := skippedEntries(t, r.h, r.id); len(skipped) != 0 {
			t.Fatalf("the trail says a step ollie performed was waived: %+v", skipped)
		}
		done, err := r.h.svc.GetTask(r.h.Ctx(), r.task.ID)
		if err != nil || done.Status != entities.TaskCompleted || variablesOf(t, r.h, r.id)["approved"] != true {
			t.Fatalf("ollie's task is %q (%v) and approved = %v; what he did must stay done", done.Status, err, variablesOf(t, r.h, r.id)["approved"])
		}
		if seen := tasksEverOn(t, r.h, r.id, "salesApprove"); seen != 1 {
			t.Fatalf("the process moved past the operations approval %d times, want once", seen)
		}
	})
}
