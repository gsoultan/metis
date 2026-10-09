package bpmn_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"gorm.io/gorm"
)

// saidOn is how a sentence writes a moment: the layout the service uses.
const saidOn = "2 January 2006 15:04 MST"

// letTimePass moves a request's deadline into the past, by so many minutes:
// the state time passing creates.
func letTimePass(t *testing.T, h engineHarness, requestID uuid.UUID, minutes int) {
	t.Helper()
	if err := h.db.Exec(`UPDATE deviation_requests SET expires_at = now() - make_interval(mins => ?) WHERE id = ?`, minutes, requestID).Error; err != nil {
		t.Fatalf("let time pass: %v", err)
	}
}

// storedRequest is a request as its table holds it, whatever the clock says.
type storedRequest struct {
	Status    string
	LiveKey   *string
	DecidedBy *string
	DecidedAt *time.Time
	ExpiresAt time.Time
}

func requestAsStored(t *testing.T, h engineHarness, requestID uuid.UUID) storedRequest {
	t.Helper()
	var stored storedRequest
	if err := h.db.Raw(`SELECT status, live_key, decided_by, decided_at, expires_at FROM deviation_requests WHERE id = ?`, requestID).
		Scan(&stored).Error; err != nil {
		t.Fatalf("read the request as it is stored: %v", err)
	}
	return stored
}

// storedLedgerRow is the ledger row of a request as its table holds it.
type storedLedgerRow struct {
	Status       string
	LiveVisitKey *string
	DecidedAt    *time.Time
}

func ledgerRowAsStored(t *testing.T, h engineHarness, requestID uuid.UUID) storedLedgerRow {
	t.Helper()
	var stored storedLedgerRow
	if err := h.db.Raw(`SELECT status, live_visit_key, decided_at FROM instance_deviations WHERE request_id = ?`, requestID).
		Scan(&stored).Error; err != nil {
		t.Fatalf("read the ledger row as it is stored: %v", err)
	}
	return stored
}

// sweep runs the expiry as the server does: as system work, at a time the
// caller gives.
func (w waiver) sweep(ctx context.Context, now time.Time) (int64, error) {
	return w.approvals.ExpireDeviationRequests(entities.WithSystemContext(ctx), now)
}

// withPatientSweep is the waiver with a sweep that waits for a held row for as
// long as a test may hold it. The server's sweep gives a request up after two
// seconds; a test that stops a sweep on a row of its own, to put two decisions
// in an order, must not have that order depend on how fast the machine is.
// The server's own wait has a test of its own.
func (w waiver) withPatientSweep() waiver {
	w.approvals = serviceimpl.NewDeviationRequestService(w.h.repo, w.h.engine, serviceimpl.WithSweepLockWait(lockWait))
	w.svc = secondedByBudi{asking: w.asking, approvals: w.approvals}
	return w
}

// sendSweep runs the expiry on a goroutine of its own, giving up after lockWait.
func (w waiver) sendSweep(now time.Time) *sent[int64] {
	return send(func() (int64, error) {
		inTime, stop := context.WithTimeout(w.h.Ctx(), lockWait)
		defer stop()
		return w.sweep(inTime, now)
	})
}

// sendRejection has an administrator reject a request on a goroutine of its
// own, giving up after lockWait.
func (w waiver) sendRejection(name string, requestID uuid.UUID) *sent[entities.DeviationRequest] {
	return send(func() (entities.DeviationRequest, error) {
		inTime, stop := context.WithTimeout(w.as(name), lockWait)
		defer stop()
		return w.approvals.RejectDeviationRequest(inTime, requestID, "not needed after all")
	})
}

// holdingTheLedgerRow holds a ledger row and changes nothing on it. Whoever
// closes a request takes the request's row and then this one, so it is where
// a closing can be stopped while it holds its request.
func (h engineHarness) holdingTheLedgerRow(t *testing.T, deviationID uuid.UUID) *heldRows {
	t.Helper()
	return h.holding(t, `UPDATE instance_deviations SET status = status WHERE id = ?`, deviationID)
}

// holdingTheRequest holds a request's row FOR UPDATE, as a decision holds it.
func (h engineHarness) holdingTheRequest(t *testing.T, requestID uuid.UUID) *heldRows {
	t.Helper()
	return h.holding(t, `WITH taken AS (SELECT id FROM deviation_requests WHERE id = ? FOR UPDATE)
		UPDATE deviation_requests r SET status = r.status FROM taken WHERE r.id = taken.id`, requestID)
}

// plainFailure reports whether err is the server's own failure: an error with
// none of the classes a caller is answered 400, 403 or 404 for.
func plainFailure(err error) bool {
	return err != nil && !errors.Is(err, apierr.ErrInvalidArgument) && !errors.Is(err, apierr.ErrForbidden) && !errors.Is(err, apierr.ErrNotFound)
}

// A request that is rejected, withdrawn by whoever asked, or left to expire
// changes nothing of the instance: the task is open in the same hands, the
// token is where it was, no value is set and nobody is told anything. What it
// writes is the request, its ledger row — which lets go of the visit — and one
// trail entry that says who ended it, or that nobody did, and when. The same
// waive can then be asked for again.
func TestARejectionAWithdrawalAndAnExpiryLeaveTheInstanceAsItWas(t *testing.T) {
	const reason = "the customer called it off"
	for how, end := range map[string]struct {
		by        string
		row       entities.DeviationStatus
		request   entities.DeviationRequestStatus
		event     string
		narrative string
	}{
		"rejected by another administrator": {"budi", entities.DeviationRejected, entities.DeviationRequestRejected, serviceimpl.EventDeviationRejected,
			"budi rejected the request to waive “Review the claim” that ana made, so nothing changed. Reason: " + reason},
		"withdrawn by whoever asked": {"ana", entities.DeviationRejected, entities.DeviationRequestRejected, serviceimpl.EventDeviationRejected,
			"ana withdrew their request to waive “Review the claim”, so nothing changed. Reason: " + reason},
		"expired": {"", entities.DeviationExpired, entities.DeviationRequestExpired, serviceimpl.EventDeviationExpired, ""},
	} {
		t.Run(how, func(t *testing.T) {
			h := newEngineHarness(t, "Closed Request Project")
			h.recordsAsProductionDoes()
			events := &eventLog{}
			h.dispatcher.Register(events)
			w := newWaiver(h)
			id := w.start(t, unroutable(h, "closed-request", entities.ExclusiveGateway, "Verdict?"), map[string]any{"verdict": "undecided"})
			task := theOpenTask(t, h, id, "review")
			cmd := deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"verdict": "accept"})
			asked := w.ask(t, cmd)
			request := asked.PendingApproval.RequestID
			deadline := asked.PendingApproval.ExpiresAt

			before, raised := everyRow(t, h), events.count()
			if end.by == "" {
				if n, err := w.sweep(h.Ctx(), time.Now().Add(73*time.Hour)); err != nil || n != 1 {
					t.Fatalf("the sweep closed %d (%v), want the one", n, err)
				}
			} else {
				got, err := w.approvals.RejectDeviationRequest(w.as(end.by), request, "  "+reason+" \n")
				if err != nil || got.Status != entities.DeviationRequestRejected || got.DecidedBy != end.by || got.DecidedByID != accountID(end.by) ||
					got.DecisionReason != reason || got.DecidedAt == nil {
					t.Fatalf("%s rejects: %+v, %v", end.by, got, err)
				}
			}

			wrote := tablesThatDiffer(before, everyRow(t, h))
			if len(wrote) != 3 || !strings.HasPrefix(wrote[0], "audit_logs ") || !strings.HasPrefix(wrote[1], "deviation_requests ") ||
				!strings.HasPrefix(wrote[2], "instance_deviations ") {
				t.Fatalf("it changed %v, want the request, its row and one entry and nothing else", wrote)
			}
			if events.count() != raised {
				t.Fatalf("it raised %d event(s); nobody is told of a waive that was never made", events.count()-raised)
			}
			if open := theOpenTask(t, h, id, "review"); open.ID != task.ID || open.AssigneeUsername() != "rita" {
				t.Fatalf("the task is %s with %q, want the one rita held", open.ID, open.AssigneeUsername())
			}
			if !h.waitingAt(h.Ctx(), t, id, "review") || variablesOf(t, h, id)["verdict"] != "undecided" {
				t.Fatalf("the instance moved, or holds verdict = %v", variablesOf(t, h, id)["verdict"])
			}

			rows := w.ledger(t, id)
			if len(rows) != 1 || rows[0].ID != asked.Deviation.ID || rows[0].Status != end.row || rows[0].ApprovedBy != "" ||
				rows[0].ApprovedByID != uuid.Nil || rows[0].Task != nil || rows[0].DecidedAt == nil || rows[0].Actor != "ana" {
				t.Fatalf("the ledger holds %+v, want the one row, %s, approved by nobody", rows, end.row)
			}
			if stored := ledgerRowAsStored(t, h, request); stored.LiveVisitKey != nil {
				t.Fatalf("the row still holds its visit (%q)", *stored.LiveVisitKey)
			}
			got, err := w.approvals.GetDeviationRequest(w.as("budi"), request)
			if err != nil || got.Status != end.request || !got.ExpiresAt.Equal(deadline) {
				t.Fatalf("the request reads %+v, %v", got, err)
			}
			stored := requestAsStored(t, h, request)
			if stored.Status != string(end.request) || stored.LiveKey != nil {
				t.Fatalf("the request is stored as %+v, want %s and holding nothing", stored, end.request)
			}
			entry := theEntryOf(t, h, id, end.event)
			want := end.narrative
			if end.by == "" {
				// Nobody decided it: it names nobody, and is dated — the row as
				// the request — by the deadline, which is when it happened.
				want = "Nobody decided the request to waive “Review the claim” that ana made before it expired on " +
					deadline.UTC().Format(saidOn) + ", so nothing changed."
				if got.DecidedBy != "" || got.DecidedByID != uuid.Nil || got.DecidedAt != nil || !rows[0].DecidedAt.Equal(deadline) {
					t.Fatalf("an expiry reads as somebody's decision, or is not dated by its deadline %v: request %+v, row decided at %v",
						deadline, got, rows[0].DecidedAt)
				}
			}
			if entry.Narrative != want {
				t.Fatalf("the entry reads\n  %s\nwant\n  %s", entry.Narrative, want)
			}
			if entry.Data["request_id"] != request.String() || entry.Data["requested_by"] != "ana" || entry.Data["deviation_id"] != rows[0].ID.String() {
				t.Fatalf("the entry does not name the request, who asked and its row: %v", entry.Data)
			}
			_, withdrawn := entry.Data["withdrawn"]
			_, rejectedBy := entry.Data["rejected_by"]
			if withdrawn != (end.by == "ana") || rejectedBy != (end.by != "") || (end.by != "" && entry.Data["rejected_by"] != end.by) {
				t.Fatalf("the entry's data says %v of who ended it; want %q, and withdrawn only when ana did", entry.Data, end.by)
			}
			// No business value: what was asked for is sealed in the request.
			for key, value := range entry.Data {
				if text, _ := value.(string); strings.Contains(text, "accept") || key == "outputs" || key == "variables" {
					t.Fatalf("the entry carries a value that was asked for: %v", entry.Data)
				}
			}

			// A decided request is history, not a lock.
			again := w.ask(t, cmd)
			if again.PendingApproval.RequestID == request || again.Replayed {
				t.Fatal("asking again answered with the request that was closed")
			}
		})
	}
}

// Rejecting is an administrator's, in the organization the request is for:
// whoever else tries changes nothing and learns nothing, and an administrator
// of another organization is told there is no such request. So is the sweep
// the server's alone: its clock is its caller's, and no request carries one.
func TestOnlyAnAdministratorOfTheOrganizationRejectsARequest(t *testing.T) {
	h := newEngineHarness(t, "Rejection Authority Project")
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-rejection-authority"), nil)
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	request := asked.PendingApproval.RequestID

	here := entities.ActingOrganization(h.Ctx())
	elsewhere, err := h.svc.CreateOrganization(t.Context(), "Another Organization", "")
	if err != nil {
		t.Fatalf("create the other organization: %v", err)
	}
	inTheOther := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: elsewhere.ID.String()})
	otto := entities.User{ID: accountID("otto"), Username: "otto", RolesByOrganization: map[uuid.UUID][]string{elsewhere.ID: {entities.RoleAdmin}}}
	before := everyRow(t, h)
	for who, caller := range map[string]struct {
		ctx  context.Context
		want error
	}{
		"nobody signed in": {h.Ctx(), apierr.ErrForbidden},
		"a member":         {signedInAs(h.Ctx(), entities.User{ID: accountID("mia"), Username: "mia", Roles: []string{entities.RoleUser}}), apierr.ErrForbidden},
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
	} {
		got, err := w.approvals.RejectDeviationRequest(caller.ctx, request, "let it go")
		if !errors.Is(err, caller.want) || got.ID != uuid.Nil {
			t.Errorf("%s rejecting: got %+v, %v; want %v and nothing of the request", who, got, err, caller.want)
		}
		// Whoever may not decide may not run the server's sweep either, with a
		// clock of their own choosing or with any.
		if n, err := w.approvals.ExpireDeviationRequests(caller.ctx, time.Now().Add(1000*time.Hour)); !plainFailure(err) || n != 0 {
			t.Errorf("%s running the expiry: %d, %v; want it refused as nothing a request may ask for", who, n, err)
		}
	}
	// An administrator of this organization is not the server either.
	if n, err := w.approvals.ExpireDeviationRequests(w.as("budi"), time.Now().Add(1000*time.Hour)); !plainFailure(err) || n != 0 {
		t.Errorf("an administrator running the expiry with a clock of their own: %d, %v; want it refused", n, err)
	}
	// What a person typed and can put right, before anything is read.
	for name, reason := range map[string]string{"no reason": "", "only spaces": " \t\n", "a reason longer than the ledger keeps": strings.Repeat("é", 2001)} {
		if _, err := w.approvals.RejectDeviationRequest(w.as("budi"), request, reason); !errors.Is(err, apierr.ErrInvalidArgument) {
			t.Errorf("rejecting with %s: %v, want it refused as the caller's to fix", name, err)
		}
	}
	if _, err := w.approvals.RejectDeviationRequest(w.as("budi"), uuid.Must(uuid.NewV7()), "no"); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("rejecting a request that does not exist: %v, want not found", err)
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("the refused rejections changed %v", changed)
	}
	if got, _ := w.approvals.GetDeviationRequest(w.ctx, request); got.Status != entities.DeviationRequestPending {
		t.Fatalf("after the refused rejections the request reads %q", got.Status)
	}

	// A reason of exactly the length the ledger keeps is kept, and an
	// administrator who holds the role in this organization alone is one.
	longest := strings.Repeat("é", 2000)
	citra := entities.User{ID: accountID("citra"), Username: "citra", RolesByOrganization: map[uuid.UUID][]string{here: {entities.RoleAdmin}}}
	got, err := w.approvals.RejectDeviationRequest(signedInAs(h.Ctx(), citra), request, longest)
	if err != nil || got.Status != entities.DeviationRequestRejected || got.DecidedBy != "citra" || got.DecisionReason != longest {
		t.Fatalf("an administrator of this organization alone rejecting: %+v, %v", got.Status, err)
	}
	// Decided, it is decided: nobody rejects or approves it again, and each is
	// told what became of it.
	for who, decide := range map[string]func() error{
		"rejecting it again": func() error {
			_, err := w.approvals.RejectDeviationRequest(w.as("budi"), request, "no")
			return err
		},
		"its requester withdrawing it": func() error { _, err := w.approvals.RejectDeviationRequest(w.ctx, request, "no"); return err },
		"approving it":                 func() error { _, err := w.approvals.ApproveDeviationRequest(w.as("budi"), request, ""); return err },
	} {
		if err := decide(); !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "citra rejected this on ") {
			t.Errorf("%s: %v, want it told that citra rejected it", who, err)
		}
	}
	if rejections := entriesOfType(t, h, id, serviceimpl.EventDeviationRejected); len(rejections) != 1 {
		t.Fatalf("the trail records %d rejections, want the one", len(rejections))
	}
}

// A request past its deadline is not rejected: the clock decided it first.
// The rejection that finds it so records the expiry — kept, though the
// rejection is refused — and names nobody as having decided.
func TestARejectionThatComesAfterTheDeadlineRecordsTheExpiry(t *testing.T) {
	h := newEngineHarness(t, "Late Rejection Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-late-rejection"), nil)
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	request := asked.PendingApproval.RequestID
	letTimePass(t, h, request, 1)
	deadline := requestAsStored(t, h, request).ExpiresAt

	for _, who := range []string{"budi", "ana"} {
		_, err := w.approvals.RejectDeviationRequest(w.as(who), request, "too late")
		want := apierr.Invalidf("This request already expired on %s.", deadline.UTC().Format(saidOn))
		if who == "ana" {
			// The second to come finds it written down.
			want = apierr.Invalidf("This request expired on %s.", deadline.UTC().Format(saidOn))
		}
		if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != want.Error() {
			t.Fatalf("%s rejecting after the deadline: %v\nwant exactly\n  %v", who, err, want)
		}
	}
	stored, row := requestAsStored(t, h, request), w.ledger(t, id)[0]
	if stored.Status != string(entities.DeviationRequestExpired) || stored.DecidedBy != nil || stored.LiveKey != nil ||
		row.Status != entities.DeviationExpired || row.DecidedAt == nil || !row.DecidedAt.Equal(deadline) {
		t.Fatalf("after the late rejection the request is stored as %+v and its row reads %q decided at %v; want both expired at the deadline, by nobody",
			stored, row.Status, row.DecidedAt)
	}
	if n := len(entriesOfType(t, h, id, serviceimpl.EventDeviationExpired)); n != 1 {
		t.Fatalf("the trail says the request expired %d times, want once", n)
	}
	if n := len(entriesOfType(t, h, id, serviceimpl.EventDeviationRejected)); n != 0 {
		t.Fatalf("the trail records %d rejection(s) of a request the clock had decided", n)
	}
	if len(openIterationTasks(h.Ctx(), t, h, id, "opsApprove")) != 1 {
		t.Fatal("the late rejection touched the step")
	}
}

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

// A request for a migration is rejected, withdrawn and expired as a waive's
// is, on the request alone: it is for a version, not an instance, so there is
// no ledger row to close and no instance whose trail would say so. (Nothing
// asks for a migration yet; the requests are written through the repository.)
func TestAMigrationRequestIsRejectedAndExpiredOnItsRowAlone(t *testing.T) {
	h := newEngineHarness(t, "Migration Request Project")
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	source, err := h.svc.CreateDefinition(h.Ctx(), opsApproval(h.projID, "ops-migration-request"))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	target, err := h.svc.CreateDefinition(h.Ctx(), opsApproval(h.projID, "ops-migration-request"))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	ask := func(fingerprint string) uuid.UUID {
		t.Helper()
		var asked entities.DeviationRequest
		err := h.repo.UnitOfWork().Do(h.Ctx(), func(txCtx context.Context) error {
			var err error
			asked, err = h.repo.DeviationRequest().Create(txCtx, entities.DeviationRequest{
				Project: &entities.Project{ID: h.projID}, Kind: entities.DeviationRequestMigration, Status: entities.DeviationRequestPending,
				SourceDefinition: &entities.ProcessDefinition{ID: source}, TargetDefinition: &entities.ProcessDefinition{ID: target},
				RequestedBy: "ana", RequestedByID: accountID("ana"), Reason: waiveReason, Fingerprint: fingerprint,
				Command: map[string]any{"node_mapping": map[string]any{}}, Plan: map[string]any{"because": []string{"“Operations approve” is skipped"}},
				ExpiresAt: time.Now().Add(time.Hour),
			})
			return err
		})
		if err != nil {
			t.Fatalf("write a request for a migration: %v", err)
		}
		return asked.ID
	}
	rejected, withdrawn, late, swept := ask("mf1-rejected"), ask("mf1-withdrawn"), ask("mf1-late"), ask("mf1-swept")
	before := everyRow(t, h)

	for who, request := range map[string]uuid.UUID{"budi": rejected, "ana": withdrawn} {
		got, err := w.approvals.RejectDeviationRequest(w.as(who), request, "not this release")
		if err != nil || got.Status != entities.DeviationRequestRejected || got.DecidedBy != who || got.DecidedByID != accountID(who) ||
			got.DecisionReason != "not this release" || got.DecidedAt == nil {
			t.Fatalf("%s rejecting a request for a migration: %+v, %v", who, got, err)
		}
		if _, err := w.approvals.RejectDeviationRequest(w.as("budi"), request, "again"); !errors.Is(err, apierr.ErrInvalidArgument) ||
			!strings.Contains(err.Error(), who+" rejected this on ") {
			t.Fatalf("rejecting it again: %v, want it told %s rejected it", err, who)
		}
	}
	letTimePass(t, h, late, 2)
	letTimePass(t, h, swept, 1)
	if _, err := w.approvals.RejectDeviationRequest(w.as("budi"), late, "too late"); !errors.Is(err, apierr.ErrInvalidArgument) ||
		!strings.Contains(err.Error(), "This request already expired on ") {
		t.Fatalf("rejecting a request for a migration after its deadline: %v", err)
	}
	if n, err := w.sweep(h.Ctx(), time.Now()); err != nil || n != 1 {
		t.Fatalf("the sweep closed %d (%v), want the one nobody had found", n, err)
	}
	for request, want := range map[uuid.UUID]entities.DeviationRequestStatus{
		rejected: entities.DeviationRequestRejected, withdrawn: entities.DeviationRequestRejected,
		late: entities.DeviationRequestExpired, swept: entities.DeviationRequestExpired,
	} {
		if stored := requestAsStored(t, h, request); stored.Status != string(want) || stored.LiveKey != nil ||
			(want == entities.DeviationRequestExpired) != (stored.DecidedBy == nil) {
			t.Fatalf("request %s is stored as %+v, want %s, holding nothing, and naming a decider only when somebody decided", request, stored, want)
		}
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 1 || !strings.HasPrefix(changed[0], "deviation_requests ") {
		t.Fatalf("closing requests for a migration changed %v, want the requests and nothing else", changed)
	}
}

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

// A decision that finds its request stale or past its deadline records that
// and then refuses, and the record is kept because the decision's transaction
// is its own. Run inside somebody else's, the record would be undone with it
// whatever the caller was told — so a decision, the closing of an overdue
// request and the sweep each refuse to run there, as the server's mistake,
// having changed nothing.
func TestADecisionRefusesToRunInsideSomebodyElsesTransaction(t *testing.T) {
	h := newEngineHarness(t, "Enclosed Decision Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-enclosed"), nil)
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	request := asked.PendingApproval.RequestID
	cmd := w.previewed(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	enclosed := func(ctx context.Context, work func(txCtx context.Context) error) error {
		return h.repo.UnitOfWork().Do(ctx, work)
	}
	refused := func(t *testing.T, what string, err error, before map[string]string) {
		t.Helper()
		if !plainFailure(err) || !strings.Contains(err.Error(), "transaction") {
			t.Fatalf("%s inside a transaction: %v, want it refused as the server's mistake, saying why", what, err)
		}
		if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
			t.Fatalf("%s inside a transaction changed %v", what, changed)
		}
	}

	before := everyRow(t, h)
	refused(t, "an approval", enclosed(w.as("budi"), func(txCtx context.Context) error {
		_, err := w.approvals.ApproveDeviationRequest(txCtx, request, "")
		return err
	}), before)
	refused(t, "a rejection", enclosed(w.as("budi"), func(txCtx context.Context) error {
		_, err := w.approvals.RejectDeviationRequest(txCtx, request, "no")
		return err
	}), before)
	refused(t, "the sweep", enclosed(entities.WithSystemContext(h.Ctx()), func(txCtx context.Context) error {
		_, err := w.approvals.ExpireDeviationRequests(txCtx, time.Now().Add(73*time.Hour))
		return err
	}), before)

	// Asking again for a waive whose request is overdue closes that request
	// in a transaction of its own, after the apply's has ended. Enclosed, the
	// apply's has not ended — it still holds the instance — so the closing is
	// refused rather than take a request's row after an instance's.
	letTimePass(t, h, request, 1)
	before = everyRow(t, h)
	refused(t, "asking again after the deadline", enclosed(w.ctx, func(txCtx context.Context) error {
		_, err := w.asking.DeviateInstance(txCtx, cmd)
		return err
	}), before)
	if stored := requestAsStored(t, h, request); stored.Status != string(entities.DeviationRequestPending) {
		t.Fatalf("the request is stored as %+v, want it untouched", stored)
	}
}

// An approval and a rejection of one request meet at the request's row, with
// the order made: whichever has the row decides, and the other — stopped
// behind it — reads what it left and is told so in words. Nothing is decided
// twice, and a waive is made only when the approval was first.
func TestAnApprovalAndARejectionTakeTurnsAtTheRequest(t *testing.T) {
	h := newEngineHarness(t, "Approve Reject Order Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	w := newWaiver(h)
	h.deploy(t, opsApproval(h.projID, "ops-approve-reject-order"))
	ask := func(t *testing.T) (uuid.UUID, entities.DeviationOutcome) {
		t.Helper()
		id, err := h.svc.StartProcess(h.Ctx(), h.projID, "ops-approve-reject-order", nil)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		return id, w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	}

	// The approval holds the request and the instance and is withdrawing the
	// task when the rejection arrives.
	t.Run("the approval first", func(t *testing.T) {
		id, asked := ask(t)
		request, withdrawn := asked.PendingApproval.RequestID, len(events.ofType(entities.EventTaskCanceled))
		held := h.holdingTheTask(t, theOpenTask(t, h, id, "opsApprove").ID)
		approval := w.sendApproval("budi", request)
		h.waitForWaiters(t, held, 1, approval.answered)
		rejection := w.sendRejection("citra", request)
		h.waitForWaiters(t, held, 2, approval.answered, rejection.answered)
		held.letGo(t, false)

		if out, err := approval.answer(t, "budi's approval"); err != nil || !out.Applied {
			t.Fatalf("the approval that got there first: %+v, %v", out, err)
		}
		got, err := rejection.answer(t, "citra's rejection")
		if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "budi approved this on ") ||
			!strings.HasSuffix(err.Error(), ", and it was applied.") || got.ID != uuid.Nil {
			t.Fatalf("the rejection that waited: %+v, %v; want it told that budi approved it and that it was applied", got.Status, err)
		}
		if stored := requestAsStored(t, h, request); stored.Status != string(entities.DeviationRequestApplied) || stored.DecidedBy == nil || *stored.DecidedBy != "budi" {
			t.Fatalf("the request is stored as %+v, want applied on budi's approval", stored)
		}
		if row := w.theWaive(t, id); row.Status != entities.DeviationApplied || len(events.ofType(entities.EventTaskCanceled)) != withdrawn+1 ||
			len(entriesOfType(t, h, id, serviceimpl.EventDeviationRejected)) != 0 {
			t.Fatalf("the row reads %q; want the waive made once and no rejection recorded", row.Status)
		}
	})

	// The rejection holds the request and is closing its ledger row when the
	// approval arrives.
	t.Run("the rejection first", func(t *testing.T) {
		id, asked := ask(t)
		request, withdrawn := asked.PendingApproval.RequestID, len(events.ofType(entities.EventTaskCanceled))
		held := h.holdingTheLedgerRow(t, asked.Deviation.ID)
		rejection := w.sendRejection("citra", request)
		h.waitForWaiters(t, held, 1, rejection.answered)
		approval := w.sendApproval("budi", request)
		h.waitForWaiters(t, held, 2, rejection.answered, approval.answered)
		held.letGo(t, false)

		if got, err := rejection.answer(t, "citra's rejection"); err != nil || got.Status != entities.DeviationRequestRejected || got.DecidedBy != "citra" {
			t.Fatalf("the rejection that got there first: %+v, %v", got.Status, err)
		}
		out, err := approval.answer(t, "budi's approval")
		if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "citra rejected this on ") || out.Applied || out.Deviation != nil {
			t.Fatalf("the approval that waited: %+v, %v; want it told that citra rejected it, having done nothing", out, err)
		}
		if row := w.theWaive(t, id); row.Status != entities.DeviationRejected || row.ApprovedBy != "" ||
			len(events.ofType(entities.EventTaskCanceled)) != withdrawn || len(skippedEntries(t, h, id)) != 0 ||
			len(openIterationTasks(h.Ctx(), t, h, id, "opsApprove")) != 1 {
			t.Fatalf("the row reads %+v; want it rejected, approved by nobody, and the step untouched", row)
		}
		if n := len(entriesOfType(t, h, id, serviceimpl.EventDeviationRejected)); n != 1 {
			t.Fatalf("the trail records %d rejections, want the one", n)
		}
	})
}
