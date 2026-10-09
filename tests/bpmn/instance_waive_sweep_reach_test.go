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

// overdueRequests starts so many instances of a deployed process, asks for the
// waive of each one's first step, and moves each request past its deadline —
// the first the longest overdue, which is the order the sweep reads them in.
func overdueRequests(t *testing.T, h engineHarness, w waiver, key string, count int) (instances, requests []uuid.UUID) {
	t.Helper()
	for i := range count {
		id, err := h.svc.StartProcess(h.Ctx(), h.projID, key, nil)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
		instances, requests = append(instances, id), append(requests, asked.PendingApproval.RequestID)
		letTimePass(t, h, asked.PendingApproval.RequestID, count-i)
	}
	return instances, requests
}

// breakThePlanOf makes a request's sealed plan one that no longer opens, by
// SQL: the stored ciphertext with its end overwritten. Nothing in the product
// writes one; a key that was lost, or a row restored from somewhere else,
// would.
func breakThePlanOf(t *testing.T, h engineHarness, requestID uuid.UUID) {
	t.Helper()
	if err := h.db.Exec(`UPDATE deviation_requests
		SET plan = to_jsonb(left(plan #>> '{}', length(plan #>> '{}') - 8) || 'AAAAAAAA') WHERE id = ?`, requestID).Error; err != nil {
		t.Fatalf("break the plan: %v", err)
	}
	if _, err := h.repo.DeviationRequest().Get(h.Ctx(), requestID); err == nil {
		t.Fatal("a request whose plan was overwritten still reads whole; the fixture broke nothing")
	}
}

// Requests the sweep cannot close only get older, and it reads the oldest
// first. So however many of them head the line, the pass has to get past
// them — every one of them — to the requests behind: a request that can be
// closed is closed in the same pass, and the pass says how many it closed and
// how many it could not. Here twenty-five requests have a ledger row that no
// longer waits (a state the product does not make, written by hand), and one
// healthy request is the newest of all.
func TestTheSweepReachesWhatIsBehindRequestsItCannotClose(t *testing.T) {
	h := newEngineHarness(t, "Sweep Reach Project")
	w := newWaiver(h)
	h.deploy(t, opsApproval(h.projID, "ops-sweep-reach"))
	const broken = 25
	instances, requests := overdueRequests(t, h, w, "ops-sweep-reach", broken+1)
	if err := h.db.Exec(`UPDATE instance_deviations SET status = 'rejected', live_visit_key = NULL WHERE request_id IN ?`, requests[:broken]).Error; err != nil {
		t.Fatalf("break the ledger rows of the first %d: %v", broken, err)
	}
	healthy, healthyInstance := requests[broken], instances[broken]

	n, err := w.sweep(h.Ctx(), time.Now())
	if stored := requestAsStored(t, h, healthy); stored.Status != string(entities.DeviationRequestExpired) || stored.LiveKey != nil {
		t.Fatalf("behind %d requests that cannot be closed, the one that can is stored as %+v after a pass (the pass answered %d, %v); want it expired",
			broken, stored, n, err)
	}
	if n != 1 || !plainFailure(err) || !strings.Contains(err.Error(), "25 requests") || !strings.Contains(err.Error(), requests[0].String()) {
		t.Fatalf("the pass closed %d and answered %v; want 1 closed and a failure that says 25 could not be, naming the first", n, err)
	}
	if entries := len(entriesOfType(t, h, healthyInstance, serviceimpl.EventDeviationExpired)); entries != 1 {
		t.Fatalf("the trail says the healthy request expired %d times, want once", entries)
	}
	// The next pass meets the same twenty-five, closes nothing, and says so again.
	if n, err := w.sweep(h.Ctx(), time.Now()); n != 0 || !plainFailure(err) || !strings.Contains(err.Error(), "25 requests") {
		t.Fatalf("a second pass closed %d and answered %v; want none closed and the same twenty-five said", n, err)
	}
	for i, request := range requests[:broken] {
		if stored := requestAsStored(t, h, request); stored.Status != string(entities.DeviationRequestPending) || stored.LiveKey == nil {
			t.Fatalf("request %d, which cannot be closed, is stored as %+v; want it as it was", i, stored)
		}
	}
}

// A request is rejected, withdrawn and closed without anybody reading what it
// asked for or what its requester was shown. So one whose stored plan no
// longer opens can still be ended, every way a request is ended — and read,
// with the plan said to be missing rather than shown as an empty one. What it
// cannot be is approved: an approval is of what was asked, and that has to be
// read whole.
func TestARequestWhoseStoredPlanNoLongerOpensCanStillBeEndedAndNotApproved(t *testing.T) {
	h := newEngineHarness(t, "Damaged Request Project")
	w := newWaiver(h)
	h.deploy(t, opsApproval(h.projID, "ops-damaged"))
	damaged := func(t *testing.T) (uuid.UUID, uuid.UUID, entities.DeviationCommand) {
		t.Helper()
		id, err := h.svc.StartProcess(h.Ctx(), h.projID, "ops-damaged", nil)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
		cmd := deviationCommand(entities.DeviationWaive, id, "opsApprove", nil)
		cmd.VisitKey = asked.Plan.VisitKey
		breakThePlanOf(t, h, asked.PendingApproval.RequestID)
		return id, asked.PendingApproval.RequestID, cmd
	}

	t.Run("read", func(t *testing.T) {
		_, request, _ := damaged(t)
		got, err := w.approvals.GetDeviationRequest(w.as("budi"), request)
		if err != nil || got.ID != request || got.Status != entities.DeviationRequestPending || got.RequestedBy != "ana" {
			t.Fatalf("reading a request whose plan no longer opens: %+v, %v; want the request", got, err)
		}
		if got.Plan != nil || got.Command == nil || got.ApprovedInstances == nil {
			t.Fatalf("it reads with plan %v, command %v, instances %v; want the plan absent — not empty — and the rest that opens",
				got.Plan, got.Command, got.ApprovedInstances)
		}
	})

	t.Run("approved by nobody", func(t *testing.T) {
		id, request, _ := damaged(t)
		before := everyRow(t, h)
		out, err := w.approvals.ApproveDeviationRequest(w.as("budi"), request, "")
		if !plainFailure(err) || out.Applied || out.Deviation != nil {
			t.Fatalf("approving a request whose plan no longer opens: %+v, %v; want it refused as the server's", out, err)
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
			id, request, cmd := damaged(t)
			// Whoever else previews or asks for the visit is told one waits,
			// in words, though its plan cannot be read.
			if _, err := w.asking.DeviateInstance(w.as("citra"), cmd); !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "already waiting for approval") {
				t.Fatalf("another administrator asking for the visit: %v, want it told one is waiting", err)
			}
			got, err := w.approvals.RejectDeviationRequest(w.as(name), request, "its plan cannot be read")
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

	t.Run("closed by whoever asks again after its deadline", func(t *testing.T) {
		id, request, cmd := damaged(t)
		letTimePass(t, h, request, 1)
		again, err := w.asking.DeviateInstance(w.ctx, cmd)
		if err != nil || again.PendingApproval == nil || again.PendingApproval.RequestID == request || again.Replayed {
			t.Fatalf("asking again after the deadline of a request whose plan no longer opens: %+v, %v; want a fresh request", again, err)
		}
		if stored := requestAsStored(t, h, request); stored.Status != string(entities.DeviationRequestExpired) || stored.LiveKey != nil {
			t.Fatalf("the damaged request is stored as %+v, want expired", stored)
		}
		if n := len(entriesOfType(t, h, id, serviceimpl.EventDeviationExpired)); n != 1 {
			t.Fatalf("the trail says it expired %d times, want once", n)
		}
	})
}

// The sweep does not wait without bound for a row somebody holds. A ledger
// row held by another transaction makes its request fail this pass — said,
// and left exactly as it was — and the pass goes on to the rest and ends. The
// next pass, with the row let go, closes it.
func TestTheSweepLeavesARequestWhoseLedgerRowIsHeldForTheNextPass(t *testing.T) {
	h := newEngineHarness(t, "Held Row Project")
	// The server's own sweep, with the wait the server gives it.
	w := newWaiver(h)
	w.approvals = serviceimpl.NewDeviationRequestService(h.repo, h.engine)
	h.deploy(t, opsApproval(h.projID, "ops-held-row"))
	instances, requests := overdueRequests(t, h, w, "ops-held-row", 2)
	var heldRow uuid.UUID
	for _, row := range w.ledger(t, instances[0]) {
		heldRow = row.ID
	}
	held := h.holdingTheLedgerRow(t, heldRow)

	began := time.Now()
	n, err := w.sendSweep(time.Now()).answer(t, "the sweep beside a held ledger row")
	if n != 1 || !plainFailure(err) || !strings.Contains(err.Error(), requests[0].String()) {
		t.Fatalf("the pass closed %d and answered %v; want the request that is free closed, and the held one said not to be", n, err)
	}
	if waited := time.Since(began); waited > lockWait/2 {
		t.Fatalf("the pass took %s beside a held row; want it to give that request up after a short wait", waited)
	}
	if stored := requestAsStored(t, h, requests[0]); stored.Status != string(entities.DeviationRequestPending) || stored.LiveKey == nil {
		t.Fatalf("the request whose row was held is stored as %+v; want it as it was", stored)
	}
	if stored := requestAsStored(t, h, requests[1]); stored.Status != string(entities.DeviationRequestExpired) {
		t.Fatalf("the request behind it is stored as %+v, want expired", stored)
	}
	held.letGo(t, false)
	if n, err := w.sweep(h.Ctx(), time.Now()); n != 1 || err != nil {
		t.Fatalf("the next pass closed %d (%v), want the one that was held", n, err)
	}
	if stored, entries := requestAsStored(t, h, requests[0]), len(entriesOfType(t, h, instances[0], serviceimpl.EventDeviationExpired)); stored.Status != string(entities.DeviationRequestExpired) || entries != 1 {
		t.Fatalf("after the next pass it is stored as %+v with %d expiry entries; want expired, recorded once", stored, entries)
	}
}
