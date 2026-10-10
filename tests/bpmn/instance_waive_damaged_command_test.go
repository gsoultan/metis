package bpmn_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// breakTheCommandOf makes a request's sealed command one that no longer
// opens, by SQL, as breakThePlanOf does a plan: the stored ciphertext with
// its end overwritten. Nothing in the product writes one; a key that was
// lost, or a row restored from somewhere else, would. Its plan still opens.
func breakTheCommandOf(t *testing.T, h engineHarness, requestID uuid.UUID) {
	t.Helper()
	if err := h.db.Exec(`UPDATE deviation_requests
		SET command = to_jsonb(left(command #>> '{}', length(command #>> '{}') - 8) || 'AAAAAAAA') WHERE id = ?`, requestID).Error; err != nil {
		t.Fatalf("break the command: %v", err)
	}
	if _, err := h.repo.DeviationRequest().Get(h.Ctx(), requestID); err == nil {
		t.Fatal("a request whose command was overwritten still reads whole; the fixture broke nothing")
	}
}

// What a request asked for is in its stored command, and only an approval
// runs it. So a request whose command no longer opens cannot be approved —
// and everything else about it works as for any request, because nothing
// else reads the command: whoever asked is answered with it again, whoever
// previews is told it waits, it is read, rejected and withdrawn, and once it
// is past its deadline whoever asks again closes it and gets a fresh one.
// The same was already true, and tested, of a plan that no longer opens;
// that the command is never needed on these paths was true by reading only.
func TestARequestWhoseStoredCommandNoLongerOpensIsAnsweredReadAndEndedAndNotApproved(t *testing.T) {
	h := newEngineHarness(t, "Damaged Command Project")
	w := newWaiver(h)
	h.deploy(t, opsApproval(h.projID, "ops-damaged-command"))
	damaged := func(t *testing.T) (uuid.UUID, uuid.UUID, entities.DeviationCommand) {
		t.Helper()
		id, err := h.svc.StartProcess(h.Ctx(), h.projID, "ops-damaged-command", nil)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
		cmd := deviationCommand(entities.DeviationWaive, id, "opsApprove", nil)
		cmd.VisitKey = asked.Plan.VisitKey
		breakTheCommandOf(t, h, asked.PendingApproval.RequestID)
		return id, asked.PendingApproval.RequestID, cmd
	}

	t.Run("the requester applying again is answered with the request that waits", func(t *testing.T) {
		_, request, cmd := damaged(t)
		before := everyRow(t, h)
		again, err := w.asking.DeviateInstance(w.ctx, cmd)
		if err != nil || !again.Replayed || again.Applied || again.PendingApproval == nil || again.PendingApproval.RequestID != request ||
			again.PendingApproval.RequestedBy != "ana" || again.PendingApproval.Status != entities.DeviationRequestPending {
			t.Fatalf("the requester's apply, sent again: %+v, %v; want the request that waits, replayed and not applied", again, err)
		}
		if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
			t.Fatalf("the replay changed %v", changed)
		}
	})

	t.Run("a preview says it waits: a warning to the requester, a refusal to anybody else", func(t *testing.T) {
		_, request, cmd := damaged(t)
		cmd.DryRun = true
		mine, err := w.asking.DeviateInstance(w.ctx, cmd)
		if err != nil || !mine.Plan.Applicable() || !strings.Contains(strings.Join(mine.Plan.Warnings, " "), "is already waiting for approval (request "+request.String()+", asked by ana") {
			t.Fatalf("the requester's preview: %+v, %v; want an applicable plan that warns of the request that waits", mine.Plan, err)
		}
		theirs, err := w.asking.DeviateInstance(w.as("citra"), cmd)
		if err != nil || theirs.Plan.Applicable() || !strings.Contains(strings.Join(theirs.Plan.Refusals, " "), "already waiting for approval (request "+request.String()+", asked by ana); approve or reject that one.") {
			t.Fatalf("another administrator's preview: %+v, %v; want a plan refused by the request that waits", theirs.Plan, err)
		}
		cmd.DryRun = false
		if _, err := w.asking.DeviateInstance(w.as("citra"), cmd); !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "already waiting for approval") {
			t.Fatalf("another administrator's apply: %v, want it told one is waiting", err)
		}
	})

	t.Run("read, with the command absent and the rest that opens", func(t *testing.T) {
		_, request, _ := damaged(t)
		got, err := w.approvals.GetDeviationRequest(w.as("budi"), request)
		if err != nil || got.ID != request || got.Status != entities.DeviationRequestPending || got.RequestedBy != "ana" {
			t.Fatalf("reading a request whose command no longer opens: %+v, %v; want the request", got, err)
		}
		if got.Command != nil || got.Plan == nil {
			t.Fatalf("it reads with command %v and plan %v; want the command absent — not empty — and the plan that opens", got.Command, got.Plan)
		}
	})

	t.Run("approved by nobody", func(t *testing.T) {
		id, request, _ := damaged(t)
		before := everyRow(t, h)
		out, err := w.approvals.ApproveDeviationRequest(w.as("budi"), request, "")
		if !plainFailure(err) || out.Applied || out.Deviation != nil {
			t.Fatalf("approving a request whose command no longer opens: %+v, %v; want it refused as the server's", out, err)
		}
		if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
			t.Fatalf("the refused approval changed %v", changed)
		}
		if stored := requestAsStored(t, h, request); stored.Status != string(entities.DeviationRequestPending) || len(openIterationTasks(h.Ctx(), t, h, id, "opsApprove")) != 1 {
			t.Fatalf("after the refused approval the request is stored as %+v; want it still waiting and the step untouched", stored)
		}
	})

	for who, name := range map[string]string{"rejected by a second administrator": "budi", "withdrawn by its requester": "ana"} {
		t.Run(who, func(t *testing.T) {
			id, request, _ := damaged(t)
			got, err := w.approvals.RejectDeviationRequest(w.as(name), request, "what it asked for cannot be read")
			if err != nil || got.Status != entities.DeviationRequestRejected || got.DecidedBy != name {
				t.Fatalf("%s rejecting it: %+v, %v", name, got.Status, err)
			}
			if stored, row := requestAsStored(t, h, request), ledgerRowAsStored(t, h, request); stored.Status != string(entities.DeviationRequestRejected) ||
				stored.LiveKey != nil || row.Status != string(entities.DeviationRejected) || row.LiveVisitKey != nil {
				t.Fatalf("the request is stored as %+v and its row as %+v; want both rejected, holding nothing", stored, row)
			}
			if n := len(entriesOfType(t, h, id, serviceimpl.EventDeviationRejected)); n != 1 {
				t.Fatalf("the trail records %d rejections, want the one", n)
			}
			w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
		})
	}

	t.Run("closed by whoever asks again after its deadline, and by the sweep", func(t *testing.T) {
		id, request, cmd := damaged(t)
		letTimePass(t, h, request, 1)
		again, err := w.asking.DeviateInstance(w.ctx, cmd)
		if err != nil || again.PendingApproval == nil || again.PendingApproval.RequestID == request || again.Replayed {
			t.Fatalf("asking again after the deadline of a request whose command no longer opens: %+v, %v; want a fresh request", again, err)
		}
		if stored := requestAsStored(t, h, request); stored.Status != string(entities.DeviationRequestExpired) || stored.LiveKey != nil {
			t.Fatalf("the damaged request is stored as %+v, want expired", stored)
		}
		if n := len(entriesOfType(t, h, id, serviceimpl.EventDeviationExpired)); n != 1 {
			t.Fatalf("the trail says it expired %d times, want once", n)
		}

		_, swept, _ := damaged(t)
		letTimePass(t, h, swept, 1)
		if n, err := w.sweep(h.Ctx(), time.Now()); err != nil || n != 1 {
			t.Fatalf("the sweep over a request whose command no longer opens closed %d (%v), want it closed", n, err)
		}
		if stored := requestAsStored(t, h, swept); stored.Status != string(entities.DeviationRequestExpired) {
			t.Fatalf("after the sweep it is stored as %+v, want expired", stored)
		}
	})
}
