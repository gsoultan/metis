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
)

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
		if swept, err := w.approvals.SweepDeviationRequests(caller.ctx, time.Now().Add(1000*time.Hour)); !plainFailure(err) || swept.Closed() != 0 {
			t.Errorf("%s running the expiry: %d, %v; want it refused as nothing a request may ask for", who, swept.Closed(), err)
		}
	}
	// An administrator of this organization is not the server either.
	if swept, err := w.approvals.SweepDeviationRequests(w.as("budi"), time.Now().Add(1000*time.Hour)); !plainFailure(err) || swept.Closed() != 0 {
		t.Errorf("an administrator running the expiry with a clock of their own: %d, %v; want it refused", swept.Closed(), err)
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
		_, err := w.approvals.SweepDeviationRequests(txCtx, time.Now().Add(73*time.Hour))
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
