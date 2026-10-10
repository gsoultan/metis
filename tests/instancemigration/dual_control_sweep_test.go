package instancemigration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// leftApproved writes what a stop between an approval and its run's report
// leaves: the request approved by omar, so long ago.
//
// It is written by SQL, onto a request that was asked for through the
// service. The state is one production makes — a request is approved (step A
// of an approval, committed) before its run begins and stays so until the
// run reports (step C) — and the moment it stands for is a server that
// stopped between the two. It is not made here by stopping a real approval
// part-way: nothing in the service lets a test stop it there, and what the
// row then holds is these four columns.
func (f *fixture) leftApproved(t *testing.T, requestID uuid.UUID, ago string) {
	t.Helper()
	if err := f.db.Exec(`UPDATE deviation_requests SET status = 'approved', decided_by = 'omar', decided_by_id = ?,
		decided_at = now() - ?::interval WHERE id = ?`, accountOf("omar"), ago, requestID).Error; err != nil {
		t.Fatalf("leave the request approved: %v", err)
	}
}

// sweep runs the server's sweep as the server does, at now.
func (f *fixture) sweep(t *testing.T, now time.Time) int64 {
	t.Helper()
	swept, err := f.svc.SweepDeviationRequests(entities.WithSystemContext(f.ctx), now)
	closed := swept.Closed()
	if err != nil {
		t.Fatalf("the sweep: %v", err)
	}
	return closed
}

// askToSkipOps parks a quotation on the operations approval and asks, as
// dita, for the migration that skips it.
func (f *fixture) askToSkipOps(t *testing.T) (dita context.Context, v1, v2 uuid.UUID, requestID uuid.UUID) {
	t.Helper()
	first, second := f.parkedOnOpsApprove(t)
	v1, v2 = uuidOf(t, first), uuidOf(t, second)
	dita = adminAs(f.ctx, "dita")
	pending, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, skipOps("the role was eliminated")...)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	return dita, v1, v2, pending.RequestID
}

// Review Focus 2: a server that stopped between approving and reporting does
// not leave the request "approved" for ever. The sweep writes down that the
// run never reported, once, and the request then holds nothing.
func TestAnApprovedRunThatNeverReportedIsMarkedInterrupted(t *testing.T) {
	f := newFixture(t)
	dita, v1, _, requestID := f.askToSkipOps(t)
	f.leftApproved(t, requestID, "2 hours")

	// Inside its hour the sweep leaves it: a run may still be going.
	if n := f.sweep(t, time.Now().Add(-90*time.Minute)); n != 0 || f.storedStatus(t, requestID) != "approved" {
		t.Fatalf("a sweep half an hour after the approval closed %d and left the request %s, want it left approved", n, f.storedStatus(t, requestID))
	}
	if n := f.sweep(t, time.Now()); n != 1 {
		t.Fatalf("the sweep closed %d, want the one request whose run never reported", n)
	}
	read, err := f.svc.GetDeviationRequest(dita, requestID)
	if err != nil || read.Status != entities.DeviationRequestInterrupted || f.storedStatus(t, requestID) != "interrupted" {
		t.Fatalf("the request reads %q (err %v), want interrupted and written so", read.Status, err)
	}
	if read.Outcome["error"] != "the run did not report back" || len(read.Outcome) != 1 {
		t.Fatalf("its outcome %v, want that the run did not report back and nothing else", read.Outcome)
	}
	if read.DecidedBy != "omar" || read.DecidedByID != accountOf("omar") {
		t.Fatalf("the sweep changed who approved: %q", read.DecidedBy)
	}
	if n := f.sweep(t, time.Now()); n != 0 {
		t.Fatalf("a second sweep closed %d more, want nothing left to close", n)
	}
	f.assertNothingMoved(t, v1)
}

// The two passes of one sweep are counted together: a request past its
// deadline and an approved one whose run never reported are both closed, each
// as what it is.
func TestOneSweepClosesWhatExpiredAndWhatNeverReported(t *testing.T) {
	f := newFixture(t)
	dita, v1, v2, unreported := f.askToSkipOps(t)
	f.leftApproved(t, unreported, "2 hours")
	// Another migration of the same versions — the same step skipped for
	// another reason — asked for and left past its deadline.
	overdue, err := f.svc.RequestMigrationApproval(dita, v1, v2, nil, skipOps("another reason")...)
	if err != nil {
		t.Fatalf("ask for another: %v", err)
	}
	f.letTimePass(t, overdue.RequestID, 1)

	// Counted apart: nobody decided the one, and somebody approved the other.
	swept, err := f.svc.SweepDeviationRequests(entities.WithSystemContext(f.ctx), time.Now())
	if err != nil || swept != (entities.SweptRequests{Expired: 1, Interrupted: 1}) || swept.Closed() != 2 {
		t.Fatalf("the sweep closed %+v (err %v), want one expired and one interrupted", swept, err)
	}
	if got := f.storedStatus(t, unreported); got != "interrupted" {
		t.Fatalf("the request whose run never reported is %s, want interrupted", got)
	}
	if got := f.storedStatus(t, overdue.RequestID); got != "expired" {
		t.Fatalf("the request past its deadline is %s, want expired", got)
	}
}

// Rulings §15. A run that is still going when its hour closes is swept to
// interrupted while it runs. The run knows what happened; the sweep knows only
// that a deadline passed. So the run's own report wins: it is written over the
// sweep's mark, and a sweep that comes after the report finds nothing to mark.
// The request's row orders the two — each takes it FOR UPDATE.
func TestARunsOwnReportWinsOverTheSweeps(t *testing.T) {
	t.Run("the sweep first", func(t *testing.T) {
		f, listing := newRacedFixture(t)
		dita, _, _, requestID := f.askToSkipOps(t)
		// The run has passed the gate and is listing its instances when, for
		// the sweep, two hours have gone by since the approval.
		sweptAs := ""
		listing.atTheApprovedApplysListing(func() {
			if n := f.sweep(t, time.Now().Add(2*time.Hour)); n != 1 {
				t.Errorf("the sweep during the run closed %d, want 1", n)
			}
			sweptAs = f.storedStatus(t, requestID)
		})
		out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), requestID, "")
		reached(t, listing)
		if sweptAs != "interrupted" {
			t.Fatalf("while the run was going the sweep left the request %q, want interrupted; the test is not exercising the order", sweptAs)
		}
		if err != nil || !out.Applied || out.MigrationResult == nil || out.MigrationResult.Changed != 1 {
			t.Fatalf("the run the sweep had marked: %+v %v, want it to finish and move the instance", out, err)
		}
		read, err := f.svc.GetDeviationRequest(dita, requestID)
		if err != nil || read.Status != entities.DeviationRequestApplied || f.storedStatus(t, requestID) != "applied" {
			t.Fatalf("after the run reported the request reads %q (err %v), want applied: the run's report wins", read.Status, err)
		}
		if read.Outcome["changed"] != float64(1) || read.Outcome["reported_after_sweep"] != true {
			t.Fatalf("the outcome %v, want what the run did and that it reported after a sweep", read.Outcome)
		}
		if _, said := read.Outcome["error"]; said {
			t.Fatalf("the outcome still says the run did not report: %v", read.Outcome)
		}
		// The instance was moved under the approval, and its records say so.
		instance := f.onlyInstance(t)
		if rows := f.ledger(t, instance.ID); len(rows) != 1 || rows[0].RequestID != requestID || rows[0].ApprovedBy != "omar" {
			t.Fatalf("the ledger of the run the sweep had marked: %+v", rows)
		}
	})

	t.Run("the report first", func(t *testing.T) {
		f := newFixture(t)
		dita, _, _, requestID := f.askToSkipOps(t)
		out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), requestID, "")
		if err != nil || !out.Applied {
			t.Fatalf("the approved run: %+v %v", out, err)
		}
		if n := f.sweep(t, time.Now().Add(2*time.Hour)); n != 0 {
			t.Fatalf("a sweep after the run reported closed %d, want nothing to mark", n)
		}
		read, err := f.svc.GetDeviationRequest(dita, requestID)
		if err != nil || read.Status != entities.DeviationRequestApplied || read.Outcome["changed"] != float64(1) {
			t.Fatalf("after the sweep the request reads %q with %v (err %v), want it applied as the run reported it", read.Status, read.Outcome, err)
		}
		if _, said := read.Outcome["reported_after_sweep"]; said {
			t.Fatalf("a run nothing swept says it reported after a sweep: %v", read.Outcome)
		}
	})

	// The run's report is written over the sweep's mark and over nothing
	// else. A request that already carries a run's report — which only a
	// second report could find, and nothing in the product makes one, so the
	// row is written here as that report would have left it — is not written
	// again: the run that comes to report leaves it as it is.
	t.Run("a report is not written over a report", func(t *testing.T) {
		f, listing := newRacedFixture(t)
		dita, _, _, requestID := f.askToSkipOps(t)
		listing.atTheApprovedApplysListing(func() {
			if err := f.db.Exec(`UPDATE deviation_requests SET status = 'interrupted', live_key = NULL,
				outcome = '{"changed": 7, "passed_over": 2, "error": "an earlier report"}' WHERE id = ?`, requestID).Error; err != nil {
				t.Errorf("write the earlier report: %v", err)
			}
		})
		out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), requestID, "")
		reached(t, listing)
		if err != nil || out.MigrationResult == nil || out.MigrationResult.Changed != 1 {
			t.Fatalf("the run: %+v %v, want it to have moved the instance", out, err)
		}
		read, err := f.svc.GetDeviationRequest(dita, requestID)
		if err != nil || read.Status != entities.DeviationRequestInterrupted || read.Outcome["changed"] != float64(7) ||
			read.Outcome["error"] != "an earlier report" {
			t.Fatalf("the request reads %q with %v (err %v), want the earlier report left as it was", read.Status, read.Outcome, err)
		}
		if _, said := read.Outcome["reported_after_sweep"]; said || !strings.Contains(string(out.Request.Status), "interrupted") {
			t.Fatalf("the run wrote over a report: %v, and answered the request as %s", read.Outcome, out.Request.Status)
		}
	})
}

// A request left approved, its window closed, is out of use three ways under
// one rule — the gate refuses it, every reader is told interrupted, and the
// sweep writes that down. The first two with no sweep having run are
// TestAnApprovedRequestPastItsWindowIsRefusedByTheGateWithNoSweep; this is the
// third, at the request's own deadline while its hour is still open.
func TestAnApprovedRequestLeftPastItsDeadlineIsOutOfUse(t *testing.T) {
	f := newFixture(t)
	_, v1, _, requestID := f.askToSkipOps(t)
	f.leftApproved(t, requestID, "10 minutes")
	f.letTimePass(t, requestID, 1)
	if n := f.sweep(t, time.Now()); n != 1 || f.storedStatus(t, requestID) != "interrupted" {
		t.Fatalf("the sweep closed %d and left the request %s, want it interrupted", n, f.storedStatus(t, requestID))
	}
	f.assertNothingMoved(t, v1)
}
