package deviation_test

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// The runbook's repair for a request the pass cannot close is SQL an operator
// runs by hand against production. It is read here from the runbook itself,
// so what is tested is what is printed, and run as the runbook says to run
// it: the first statement must report one row, or the operator rolls back and
// stops.

// repairStatements is the statements of the transaction docs/runbooks.md
// gives for closing a request the pass cannot, in order, with the request's
// id put in.
func repairStatements(t *testing.T, requestID string) []string {
	t.Helper()
	raw, err := os.ReadFile("../../docs/runbooks.md")
	if err != nil {
		t.Fatalf("read the runbook: %v", err)
	}
	_, after, found := strings.Cut(string(raw), "close both in one transaction")
	if !found {
		t.Fatal("the runbook no longer has the repair this test runs")
	}
	_, block, found := strings.Cut(after, "```sql\nBEGIN;\n")
	if !found {
		t.Fatal("the runbook's repair is no longer one transaction")
	}
	block, _, found = strings.Cut(block, "COMMIT;\n```")
	if !found {
		t.Fatal("the runbook's repair does not end in a commit")
	}
	// Comments first, as psql drops them: one of them holds a semicolon.
	var lines []string
	for _, line := range strings.Split(block, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	var statements []string
	for _, statement := range strings.Split(strings.Join(lines, "\n"), ";") {
		if strings.TrimSpace(statement) != "" {
			statements = append(statements, strings.ReplaceAll(statement, "<request-id>", requestID))
		}
	}
	if len(statements) != 2 {
		t.Fatalf("the runbook's repair has %d statements, want the two this test knows how to run: %q", len(statements), statements)
	}
	return statements
}

// repair runs the runbook's transaction as it says to: the first statement
// must report one row, otherwise it is rolled back and nothing more is run.
// It answers what each statement that ran reported, and whether it committed.
func (h *deviationHarness) repair(t *testing.T, requestID string) (reported []int64, committed bool) {
	t.Helper()
	statements := repairStatements(t, requestID)
	tx := h.db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin: %v", tx.Error)
	}
	for i, statement := range statements {
		done := tx.Exec(statement)
		if done.Error != nil {
			tx.Rollback()
			t.Fatalf("the runbook's statement %d failed: %v\n%s", i+1, done.Error, statement)
		}
		reported = append(reported, done.RowsAffected)
		if i == 0 && done.RowsAffected != 1 {
			if err := tx.Rollback().Error; err != nil {
				t.Fatalf("roll back: %v", err)
			}
			return reported, false
		}
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit: %v", err)
	}
	return reported, true
}

func TestTheRunbooksRepairClosesAnOverdueRequestAndNoOther(t *testing.T) {
	h := newDeviationRouteHarness(t)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	rowOf := func(requestID string) (status string, live *string, decidedAtDeadline bool) {
		t.Helper()
		if err := h.db.Raw(`SELECT d.status, d.live_visit_key, d.decided_at IS NOT DISTINCT FROM r.expires_at
			FROM instance_deviations d JOIN deviation_requests r ON r.id = d.request_id WHERE r.id = ?`, requestID).
			Row().Scan(&status, &live, &decidedAtDeadline); err != nil {
			t.Fatalf("read the ledger row of request %s: %v", requestID, err)
		}
		return status, live, decidedAtDeadline
	}

	// Not yet due: the repair must change nothing. Run as written before the
	// request statement came first, it closed the ledger row under a request
	// that still waited — the stuck state it is there to repair.
	t.Run("a request that is not yet due is left exactly as it was", func(t *testing.T) {
		instanceID := h.oneStep(t)
		requestID := h.askToWaive(t, boss, instanceID)
		before := h.everyRow(t)
		reported, committed := h.repair(t, requestID)
		if committed || len(reported) != 1 || reported[0] != 0 {
			t.Fatalf("the repair on a request not yet due reported %v and committed=%v, want UPDATE 0 and a rollback", reported, committed)
		}
		h.requireUnchanged(t, before, "the repair, rolled back")
		// And pasted whole into psql, which does not stop at an UPDATE 0:
		// both statements run and the transaction commits. The runbook says
		// the second statement then changes nothing either, and it does not.
		pasted := h.db.Begin()
		for i, statement := range repairStatements(t, requestID) {
			done := pasted.Exec(statement)
			if done.Error != nil || done.RowsAffected != 0 {
				pasted.Rollback()
				t.Fatalf("pasted whole, statement %d reported %d rows (%v) on a request not yet due, want UPDATE 0", i+1, done.RowsAffected, done.Error)
			}
		}
		if err := pasted.Commit().Error; err != nil {
			t.Fatalf("commit the pasted block: %v", err)
		}
		h.requireUnchanged(t, before, "the repair, pasted whole and committed")
		if status, live, _ := rowOf(requestID); status != "pending_approval" || live == nil || h.requestStatus(t, requestID) != "pending_approval" {
			t.Fatalf("after the rollback the ledger row is %s and the request %s, want both still waiting and the visit held", status, h.requestStatus(t, requestID))
		}
		// And it is still a request somebody can approve.
		if status, approved, raw := h.decide(t, deputy, requestID, "approve", ""); status != http.StatusOK || !approved.Applied {
			t.Fatalf("approving the request the repair left alone: %d (%s)", status, raw)
		}
	})

	// Past its deadline and still stored as waiting, with its ledger row
	// holding the visit: the rows of a request the pass could not close.
	t.Run("a request past its deadline is closed with its ledger row, and the visit is free", func(t *testing.T) {
		instanceID := h.oneStep(t)
		requestID := h.askToWaive(t, boss, instanceID)
		h.letTheDeadlinePass(t, requestID)
		reported, committed := h.repair(t, requestID)
		if !committed || len(reported) != 2 || reported[0] != 1 || reported[1] != 1 {
			t.Fatalf("the repair on an overdue request reported %v and committed=%v, want UPDATE 1 twice and a commit", reported, committed)
		}
		status, live, atDeadline := rowOf(requestID)
		if status != "expired" || live != nil || !atDeadline || h.requestStatus(t, requestID) != "expired" {
			t.Fatalf("after the repair the ledger row is %s (visit %v, dated by the deadline: %v) and the request %s; want both expired, the visit let go, dated by the deadline",
				status, live, atDeadline, h.requestStatus(t, requestID))
		}
		var liveKey *string
		if err := h.db.Raw(`SELECT live_key FROM deviation_requests WHERE id = ?`, requestID).Row().Scan(&liveKey); err != nil || liveKey != nil {
			t.Fatalf("the request still holds its key (%v, err %v)", liveKey, err)
		}
		// Run again, it finds nothing to do and is rolled back.
		if reported, committed := h.repair(t, requestID); committed || reported[0] != 0 {
			t.Fatalf("the repair run twice reported %v and committed=%v, want UPDATE 0 and a rollback", reported, committed)
		}
		// The step can be asked to be waived again, which is what the repair is for.
		if again := h.askToWaive(t, boss, instanceID); again == requestID {
			t.Fatal("asking again answered the request that was closed")
		}
	})
}
