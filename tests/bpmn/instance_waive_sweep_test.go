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

// A rejection and the sweep both take the request's row before they write, so
// whichever has it decides and the other finds what it left — or, for the
// sweep, passes the request by without waiting: a row somebody holds is being
// decided now. Each order is made, not hoped for: the first is stopped at the
// ledger row, holding the request, before the second is sent.
func TestARejectionAndTheSweepNeverBothDecideARequest(t *testing.T) {
	h := newEngineHarness(t, "Reject Sweep Order Project")
	w := newWaiver(h).withPatientSweep()
	h.deploy(t, opsApproval(h.projID, "ops-reject-sweep"))
	later := time.Now().Add(73 * time.Hour)
	waiting := func(t *testing.T) (id, request uuid.UUID, held *heldRows) {
		t.Helper()
		id, err := h.svc.StartProcess(h.Ctx(), h.projID, "ops-reject-sweep", nil)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
		return id, asked.PendingApproval.RequestID, h.holdingTheLedgerRow(t, asked.Deviation.ID)
	}
	recorded := func(t *testing.T, id uuid.UUID, expiries, rejections int) {
		t.Helper()
		if got := len(entriesOfType(t, h, id, serviceimpl.EventDeviationExpired)); got != expiries {
			t.Fatalf("the trail says the request expired %d time(s), want %d", got, expiries)
		}
		if got := len(entriesOfType(t, h, id, serviceimpl.EventDeviationRejected)); got != rejections {
			t.Fatalf("the trail records %d rejection(s), want %d", got, rejections)
		}
		if len(w.ledger(t, id)) != 1 || len(openIterationTasks(h.Ctx(), t, h, id, "opsApprove")) != 1 {
			t.Fatal("the ledger holds another row, or the step was touched")
		}
	}

	t.Run("the sweep first", func(t *testing.T) {
		id, request, held := waiting(t)
		sweep := w.sendSweep(later)
		h.waitForWaiters(t, held, 1, sweep.answered)
		rejection := w.sendRejection("budi", request)
		h.waitForWaiters(t, held, 2, sweep.answered, rejection.answered)
		held.letGo(t, false)

		if n, err := sweep.answer(t, "the sweep"); err != nil || n != 1 {
			t.Fatalf("the sweep that got there first closed %d (%v), want the one", n, err)
		}
		if _, err := rejection.answer(t, "budi's rejection"); !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "This request expired on ") {
			t.Fatalf("the rejection that waited: %v, want it told the request expired", err)
		}
		if stored := requestAsStored(t, h, request); stored.Status != string(entities.DeviationRequestExpired) || stored.DecidedBy != nil {
			t.Fatalf("the request is stored as %+v, want expired by nobody", stored)
		}
		recorded(t, id, 1, 0)
	})

	t.Run("the rejection first", func(t *testing.T) {
		id, request, held := waiting(t)
		rejection := w.sendRejection("budi", request)
		h.waitForWaiters(t, held, 1, rejection.answered)
		// The sweep does not wait for a request somebody is deciding: it passes
		// it by, and answers while the rejection is still where it was stopped.
		if n, err := w.sendSweep(later).answer(t, "the sweep beside a rejection"); err != nil || n != 0 || rejection.answered() {
			t.Fatalf("the sweep beside a rejection in flight closed %d (%v), rejection finished %v; want it to pass the request by", n, err, rejection.answered())
		}
		held.letGo(t, false)

		if got, err := rejection.answer(t, "budi's rejection"); err != nil || got.Status != entities.DeviationRequestRejected {
			t.Fatalf("the rejection that got there first: %+v, %v", got.Status, err)
		}
		if n, err := w.sweep(h.Ctx(), later); err != nil || n != 0 {
			t.Fatalf("a sweep after the rejection closed %d (%v); a rejected request is not expired as well", n, err)
		}
		if stored := requestAsStored(t, h, request); stored.Status != string(entities.DeviationRequestRejected) {
			t.Fatalf("the request is stored as %+v, want rejected", stored)
		}
		recorded(t, id, 0, 1)
	})

	t.Run("the rejection of an overdue request first", func(t *testing.T) {
		id, request, held := waiting(t)
		letTimePass(t, h, request, 1)
		rejection := w.sendRejection("budi", request)
		h.waitForWaiters(t, held, 1, rejection.answered)
		if n, err := w.sendSweep(time.Now()).answer(t, "the sweep beside a late rejection"); err != nil || n != 0 || rejection.answered() {
			t.Fatalf("the sweep beside a late rejection closed %d (%v); want it to pass the request by", n, err)
		}
		held.letGo(t, false)

		if _, err := rejection.answer(t, "budi's late rejection"); !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "This request already expired on ") {
			t.Fatalf("the late rejection: %v, want it told the request had expired", err)
		}
		if n, err := w.sweep(h.Ctx(), time.Now()); err != nil || n != 0 {
			t.Fatalf("a sweep after the late rejection closed %d (%v); the expiry is recorded once", n, err)
		}
		recorded(t, id, 1, 0)
	})
}

// The sweep and an approval meet at the request's row too. An approval that
// has it — and is in the middle of its waive — is passed by, though the
// sweep's clock says the request is overdue: what the approval writes wins.
// A sweep that has it finishes, and the approval that waited is told the
// request expired and waives nothing.
func TestTheSweepAndAnApprovalNeverBothDecideARequest(t *testing.T) {
	h := newEngineHarness(t, "Sweep Approval Order Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	w := newWaiver(h).withPatientSweep()
	h.deploy(t, opsApproval(h.projID, "ops-sweep-approval"))
	later := time.Now().Add(73 * time.Hour)
	ask := func(t *testing.T) (uuid.UUID, entities.DeviationOutcome) {
		t.Helper()
		id, err := h.svc.StartProcess(h.Ctx(), h.projID, "ops-sweep-approval", nil)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		return id, w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	}

	t.Run("the approval first", func(t *testing.T) {
		id, asked := ask(t)
		request, withdrawn := asked.PendingApproval.RequestID, len(events.ofType(entities.EventTaskCanceled))
		held := h.holdingTheTask(t, theOpenTask(t, h, id, "opsApprove").ID)
		approval := w.sendApproval("budi", request)
		h.waitForWaiters(t, held, 1, approval.answered)
		if n, err := w.sendSweep(later).answer(t, "the sweep beside an approval"); err != nil || n != 0 || approval.answered() {
			t.Fatalf("the sweep beside an approval in flight closed %d (%v), approval finished %v; want it to pass the request by", n, err, approval.answered())
		}
		held.letGo(t, false)

		if out, err := approval.answer(t, "budi's approval"); err != nil || !out.Applied {
			t.Fatalf("the approval that got there first: %+v, %v", out, err)
		}
		if n, err := w.sweep(h.Ctx(), later); err != nil || n != 0 {
			t.Fatalf("a sweep after the approval closed %d (%v); an applied request does not expire", n, err)
		}
		if stored := requestAsStored(t, h, request); stored.Status != string(entities.DeviationRequestApplied) {
			t.Fatalf("the request is stored as %+v, want applied", stored)
		}
		if row := w.theWaive(t, id); row.Status != entities.DeviationApplied || len(events.ofType(entities.EventTaskCanceled)) != withdrawn+1 ||
			len(entriesOfType(t, h, id, serviceimpl.EventDeviationExpired)) != 0 {
			t.Fatalf("the row reads %q; want the waive made once and no expiry recorded", row.Status)
		}
	})

	t.Run("the sweep first", func(t *testing.T) {
		id, asked := ask(t)
		request, withdrawn := asked.PendingApproval.RequestID, len(events.ofType(entities.EventTaskCanceled))
		held := h.holdingTheLedgerRow(t, asked.Deviation.ID)
		sweep := w.sendSweep(later)
		h.waitForWaiters(t, held, 1, sweep.answered)
		approval := w.sendApproval("budi", request)
		h.waitForWaiters(t, held, 2, sweep.answered, approval.answered)
		held.letGo(t, false)

		if n, err := sweep.answer(t, "the sweep"); err != nil || n != 1 {
			t.Fatalf("the sweep that got there first closed %d (%v), want the one", n, err)
		}
		out, err := approval.answer(t, "budi's approval")
		if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "This request expired on ") || out.Applied || out.Deviation != nil {
			t.Fatalf("the approval that waited: %+v, %v; want it told the request expired, having done nothing", out, err)
		}
		if row := w.theWaive(t, id); row.Status != entities.DeviationExpired || len(events.ofType(entities.EventTaskCanceled)) != withdrawn ||
			len(skippedEntries(t, h, id)) != 0 || len(openIterationTasks(h.Ctx(), t, h, id, "opsApprove")) != 1 {
			t.Fatalf("the row reads %q; want it expired and the step untouched", row.Status)
		}
		if n := len(entriesOfType(t, h, id, serviceimpl.EventDeviationExpired)); n != 1 {
			t.Fatalf("the trail says the request expired %d times, want once", n)
		}
	})
}

// Every replica sweeps. Two sweeps at once close each overdue request once:
// each takes the rows it closes, and passes by a row the other holds rather
// than wait for it.
func TestTwoSweepsAtOnceCloseEachRequestOnce(t *testing.T) {
	h := newEngineHarness(t, "Two Sweeps Project")
	w := newWaiver(h).withPatientSweep()
	h.deploy(t, opsApproval(h.projID, "ops-two-sweeps"))
	type waits struct {
		id, request, row uuid.UUID
	}
	// overdue makes so many requests past their deadline, the first the
	// longest overdue: the order the sweep reads them in.
	overdue := func(t *testing.T, count int) []waits {
		t.Helper()
		made := make([]waits, count)
		for i := range made {
			id, err := h.svc.StartProcess(h.Ctx(), h.projID, "ops-two-sweeps", nil)
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
			made[i] = waits{id, asked.PendingApproval.RequestID, asked.Deviation.ID}
			letTimePass(t, h, made[i].request, count-i)
		}
		return made
	}
	closedOnce := func(t *testing.T, made []waits) {
		t.Helper()
		for i, one := range made {
			if stored := requestAsStored(t, h, one.request); stored.Status != string(entities.DeviationRequestExpired) || stored.LiveKey != nil {
				t.Fatalf("request %d is stored as %+v, want expired", i, stored)
			}
			if row := ledgerRowAsStored(t, h, one.request); row.Status != string(entities.DeviationExpired) || row.LiveVisitKey != nil {
				t.Fatalf("the row of request %d is stored as %+v, want expired", i, row)
			}
			if n := len(entriesOfType(t, h, one.id, serviceimpl.EventDeviationExpired)); n != 1 {
				t.Fatalf("the trail says request %d expired %d times, want once", i, n)
			}
		}
	}

	t.Run("sent together", func(t *testing.T) {
		made := overdue(t, 5)
		first, second := w.sendSweep(time.Now()), w.sendSweep(time.Now())
		a, errA := first.answer(t, "one sweep")
		b, errB := second.answer(t, "the other sweep")
		if errA != nil || errB != nil || a+b != int64(len(made)) {
			t.Fatalf("two sweeps closed %d and %d (%v, %v), want %d between them", a, b, errA, errB, len(made))
		}
		closedOnce(t, made)
	})

	t.Run("one stopped while the other runs", func(t *testing.T) {
		made := overdue(t, 3)
		held := h.holdingTheLedgerRow(t, made[0].row)
		stopped := w.sendSweep(time.Now())
		h.waitForWaiters(t, held, 1, stopped.answered)
		// The other sweep does not queue behind it: it closes what is free.
		if n, err := w.sendSweep(time.Now()).answer(t, "the sweep that runs"); err != nil || n != 2 || stopped.answered() {
			t.Fatalf("the sweep beside a stopped one closed %d (%v), want the two the other does not hold", n, err)
		}
		held.letGo(t, false)
		if n, err := stopped.answer(t, "the sweep that was stopped"); err != nil || n != 1 {
			t.Fatalf("the stopped sweep closed %d (%v), want the one it held", n, err)
		}
		closedOnce(t, made)
	})
}

// One request the sweep cannot close does not stop it. Here the longest
// overdue request has a ledger row that no longer waits — a state the product
// does not make, written by hand, as a repair gone wrong might leave it — so
// its closing fails. The sweep closes the others all the same, each in a
// transaction of its own, says that one could not be closed, and leaves that
// one exactly as it was: nothing of the failed closing is kept.
func TestOneRequestThatCannotBeClosedDoesNotStopTheSweep(t *testing.T) {
	h := newEngineHarness(t, "Broken Request Project")
	w := newWaiver(h)
	h.deploy(t, opsApproval(h.projID, "ops-broken-request"))
	var ids, requests []uuid.UUID
	for i := range 3 {
		id, err := h.svc.StartProcess(h.Ctx(), h.projID, "ops-broken-request", nil)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
		ids, requests = append(ids, id), append(requests, asked.PendingApproval.RequestID)
		letTimePass(t, h, asked.PendingApproval.RequestID, 3-i)
	}
	broken := requests[0]
	if err := h.db.Exec(`UPDATE instance_deviations SET status = 'rejected', live_visit_key = NULL WHERE request_id = ?`, broken).Error; err != nil {
		t.Fatalf("break the first request's ledger row: %v", err)
	}

	for pass, closes := range []int64{2, 0} {
		n, err := w.sweep(h.Ctx(), time.Now())
		if n != closes || !plainFailure(err) || !strings.Contains(err.Error(), broken.String()) {
			t.Fatalf("pass %d closed %d and answered %v; want %d closed and a failure that names request %s", pass+1, n, err, closes, broken)
		}
	}
	if stored := requestAsStored(t, h, broken); stored.Status != string(entities.DeviationRequestPending) || stored.LiveKey == nil {
		t.Fatalf("the request that could not be closed is stored as %+v; want it as it was, nothing of the failed closing kept", stored)
	}
	if n := len(entriesOfType(t, h, ids[0], serviceimpl.EventDeviationExpired)); n != 0 {
		t.Fatalf("the trail says the request that could not be closed expired %d time(s)", n)
	}
	// It still reads as what the clock made it, and nobody can approve it.
	if got, err := w.approvals.GetDeviationRequest(w.as("budi"), broken); err != nil || got.Status != entities.DeviationRequestExpired {
		t.Fatalf("the request that could not be closed reads %q (%v), want expired", got.Status, err)
	}
	if _, err := w.approvals.ApproveDeviationRequest(w.as("budi"), broken, ""); err == nil {
		t.Fatal("a request past its deadline whose row is broken was approved")
	}
	for i := 1; i < 3; i++ {
		if stored := requestAsStored(t, h, requests[i]); stored.Status != string(entities.DeviationRequestExpired) || stored.LiveKey != nil {
			t.Fatalf("request %d, behind the broken one, is stored as %+v; want expired", i, stored)
		}
		if n := len(entriesOfType(t, h, ids[i], serviceimpl.EventDeviationExpired)); n != 1 {
			t.Fatalf("the trail says request %d expired %d times, want once", i, n)
		}
	}
}

// The sweep is the server's, across every organization, and reaches a
// request whose project has been deleted: its ledger row still holds a visit,
// and nobody in the organization can read the request to reject it.
func TestTheSweepReachesARequestWhoseProjectWasDeleted(t *testing.T) {
	h := newEngineHarness(t, "Deleted Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-deleted-project"), nil)
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	request := asked.PendingApproval.RequestID
	if err := h.db.Exec(`UPDATE projects SET deleted_at = now() WHERE id = ?`, h.projID).Error; err != nil {
		t.Fatalf("delete the project: %v", err)
	}
	if _, err := w.approvals.GetDeviationRequest(w.as("budi"), request); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("reading a request of a deleted project: %v, want not found", err)
	}
	// Run for the organization, a pass reads what the organization reads, and
	// the deleted project's request is not among it.
	if n, err := w.sweep(h.Ctx(), time.Now().Add(73*time.Hour)); err != nil || n != 0 {
		t.Fatalf("a sweep run for the organization closed %d (%v) of a project it no longer reads", n, err)
	}
	// The server's pass is for no organization, as the retention loop's is.
	if n, err := w.sweep(t.Context(), time.Now().Add(73*time.Hour)); err != nil || n != 1 {
		t.Fatalf("the server's sweep closed %d (%v), want the one", n, err)
	}
	if stored := requestAsStored(t, h, request); stored.Status != string(entities.DeviationRequestExpired) || stored.LiveKey != nil {
		t.Fatalf("the request is stored as %+v, want expired", stored)
	}
	if row := ledgerRowAsStored(t, h, request); row.Status != string(entities.DeviationExpired) || row.LiveVisitKey != nil || row.DecidedAt == nil {
		t.Fatalf("the ledger row is stored as %+v, want expired and holding nothing", row)
	}
	var entries int
	if err := h.db.Raw(`SELECT count(*) FROM audit_logs WHERE instance_id = ? AND type = ?`, id, serviceimpl.EventDeviationExpired).Scan(&entries).Error; err != nil || entries != 1 {
		t.Fatalf("the trail holds %d expiry entries for the instance (%v), want one", entries, err)
	}
}
