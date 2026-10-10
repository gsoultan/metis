package bpmn_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"gorm.io/gorm"
)

// Nothing that blocks an act is found only by trying it. A preview of a waive
// says when a request already waits for the visit: to whoever made that very
// request it is a warning — applying again answers with it — and to anybody
// else, or for anything else on the visit, it is the refusal an apply would
// give, in the apply's words. A request past its deadline does not count:
// asking again closes it. A preview still reads and does nothing else.
func TestAPreviewSaysWhenARequestAlreadyWaitsForTheVisit(t *testing.T) {
	h := newEngineHarness(t, "Preview Of A Waiting Visit Project")
	w := newWaiver(h)
	id := w.start(t, unroutable(h, "preview-waiting", entities.ExclusiveGateway, "Verdict?"), nil)
	cmd := deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"verdict": "accept"})
	other := deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"verdict": "reject"})
	previewAs := func(ctx context.Context, cmd entities.DeviationCommand) entities.DeviationPlan {
		t.Helper()
		cmd.DryRun = true
		out, err := w.asking.DeviateInstance(ctx, cmd)
		if err != nil || out.Applied || out.Deviation != nil || out.PendingApproval != nil {
			t.Fatalf("preview: %+v, %v", out, err)
		}
		return out.Plan
	}
	clean := previewAs(w.ctx, cmd)
	if !clean.Applicable() {
		t.Fatalf("with nothing waiting the plan refuses:%s", lines(clean.Refusals))
	}

	asked := w.ask(t, cmd)
	request := asked.PendingApproval.RequestID
	before := everyRow(t, h)
	waiting := "A request to waive “Review the claim” is already waiting for approval (request " + request.String() + ", asked by ana); approve or reject that one."
	yours := "A request to waive “Review the claim” is already waiting for approval (request " + request.String() + ", asked by ana, until " +
		asked.PendingApproval.ExpiresAt.UTC().Format(saidOn) + "). Applying this again answers with that request and makes no second one."

	mine := previewAs(w.ctx, cmd)
	if !mine.Applicable() || !said(mine.Warnings, yours) || len(mine.Warnings) != len(clean.Warnings)+1 || mine.VisitKey != clean.VisitKey {
		t.Fatalf("the requester previewing her own request again: refusals%s\nwarnings%s\nwant it applicable, and warned exactly\n  %s",
			lines(mine.Refusals), lines(mine.Warnings), yours)
	}
	for who, plan := range map[string]entities.DeviationPlan{
		"another administrator":                       previewAs(w.as("budi"), cmd),
		"somebody else who is called ana":             previewAs(w.asAccount("ana", accountID("citra")), cmd),
		"the requester asking for another value":      previewAs(w.ctx, other),
		"another administrator asking for another":    previewAs(w.as("budi"), other),
		"the requester renamed, asking for something": previewAs(w.asAccount("ana-renamed", accountID("ana")), other),
	} {
		if plan.Applicable() || !said(plan.Refusals, waiting) || len(plan.Refusals) != 1 || len(plan.Warnings) != len(clean.Warnings) {
			t.Errorf("%s previewing a visit that waits: refusals%s\nwant exactly\n  %s", who, lines(plan.Refusals), waiting)
		}
		// What the preview refuses is what the apply answers, word for word.
	}
	_, err := w.asking.DeviateInstance(w.as("budi"), w.previewed(t, cmd))
	if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != apierr.Invalidf("%s", waiting).Error() {
		t.Fatalf("the apply the preview refused answers %v\nwant the preview's words\n  %s", err, waiting)
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("previewing a visit that waits changed %v", changed)
	}

	// Past its deadline the request holds nothing: the plan is the one a
	// preview gave before anybody asked, for the requester and for anybody.
	letTimePass(t, h, request, 1)
	for who, ctx := range map[string]context.Context{"the requester": w.ctx, "another administrator": w.as("budi")} {
		if plan := previewAs(ctx, cmd); !plan.Applicable() || len(plan.Warnings) != len(clean.Warnings) {
			t.Errorf("%s previewing after the deadline: refusals%s\nwarnings%s\nwant the plan as it was before anybody asked",
				who, lines(plan.Refusals), lines(plan.Warnings))
		}
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 1 || !strings.HasPrefix(changed[0], "deviation_requests ") {
		t.Fatalf("previewing after the deadline changed %v; a preview records nothing, the expiry included", changed)
	}
}

// Ruling 43, with the order made. Two administrators ask again for a waive
// whose request is past its deadline. Each finds it overdue, lets go of the
// instance, and goes to close the request under the request's own row — never
// holding the instance while it waits for that row, which is the order every
// decision keeps. One closes it, once; one of the two then makes the fresh
// request, and the other meets a request that is live and is answered as
// anybody who asks for a visit that waits.
func TestTwoAsksAfterTheDeadlineCloseTheRequestOnceAndOneOfThemWaits(t *testing.T) {
	h := newEngineHarness(t, "Two Asks After The Deadline Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-two-asks"), nil)
	first := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	overdue := first.PendingApproval.RequestID
	letTimePass(t, h, overdue, 1)
	cmd := w.previewed(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	askAs := func(name string) *sent[entities.DeviationOutcome] {
		return send(func() (entities.DeviationOutcome, error) {
			inTime, stop := context.WithTimeout(w.as(name), lockWait)
			defer stop()
			return w.asking.DeviateInstance(inTime, cmd)
		})
	}

	held := h.holdingTheRequest(t, overdue)
	asks := map[string]*sent[entities.DeviationOutcome]{"ana": askAs("ana")}
	h.waitForWaiters(t, held, 1, asks["ana"].answered)
	// Waiting for the request's row, she holds no instance.
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		return tx.Exec(`SELECT id FROM process_instances WHERE id = ? FOR UPDATE NOWAIT`, id).Error
	}); err != nil {
		t.Fatalf("while an ask waits for the overdue request's row, the instance's row answers %v; want it free", err)
	}
	asks["budi"] = askAs("budi")
	h.waitForWaiters(t, held, 2, asks["ana"].answered, asks["budi"].answered)
	held.letGo(t, false)

	var winner string
	var fresh entities.DeviationOutcome
	failures := map[string]error{}
	for name, ask := range asks {
		out, err := ask.answer(t, name+"'s ask")
		if err != nil {
			failures[name] = err
			continue
		}
		winner, fresh = name, out
	}
	if len(failures) != 1 || winner == "" {
		t.Fatalf("two asks after the deadline: %d refused (%v), want exactly one answered with a fresh request", len(failures), failures)
	}
	if fresh.Applied || fresh.Replayed || fresh.PendingApproval == nil || fresh.PendingApproval.RequestID == overdue ||
		fresh.PendingApproval.RequestedBy != winner {
		t.Fatalf("%s's ask was answered %+v, want a fresh request of their own", winner, fresh)
	}
	for loser, err := range failures {
		want := apierr.Invalidf("A request to waive “Operations approve” is already waiting for approval (request %s, asked by %s); approve or reject that one.",
			fresh.PendingApproval.RequestID, winner)
		if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != want.Error() {
			t.Fatalf("%s's ask, which met %s's fresh request: %v\nwant exactly\n  %v", loser, winner, err, want)
		}
	}
	rows := w.ledger(t, id)
	if len(rows) != 2 || rows[0].Status != entities.DeviationExpired || rows[1].Status != entities.DeviationPendingApproval ||
		rows[1].RequestID != fresh.PendingApproval.RequestID {
		t.Fatalf("the ledger holds %+v, want the expired request and then the fresh one", rows)
	}
	if n := len(entriesOfType(t, h, id, serviceimpl.EventDeviationExpired)); n != 1 {
		t.Fatalf("the trail says the request expired %d times, want once", n)
	}
	if stored := requestAsStored(t, h, overdue); stored.Status != string(entities.DeviationRequestExpired) || stored.DecidedBy != nil {
		t.Fatalf("the overdue request is stored as %+v, want expired by nobody", stored)
	}
	if len(openIterationTasks(h.Ctx(), t, h, id, "opsApprove")) != 1 {
		t.Fatal("asking again touched the step")
	}
}
