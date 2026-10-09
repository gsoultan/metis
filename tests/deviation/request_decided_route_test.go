package deviation_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Rulings §6: nobody decides a request that has expired, one that no longer
// holds, or one somebody has already decided, and the refusal says which —
// over the API as in the service. A request found expired or stale by the
// decision that came for it is closed as that, and stays closed: the 400
// arrives and the record of why is kept.
func TestADecidedOrExpiredRequestIsRefusedOverTheAPI(t *testing.T) {
	h := newDeviationRouteHarness(t)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	ledgerStatus := func(instanceID uuid.UUID) string {
		t.Helper()
		_, ledger, raw := h.readDeviations(t, deputy, instanceID.String())
		if len(ledger.Deviations) != 1 {
			t.Fatalf("the ledger holds %d rows, want the one that waited: %s", len(ledger.Deviations), raw)
		}
		return fmt.Sprint(ledger.Deviations[0]["status"])
	}

	// Past its deadline, and no sweep has come round: it reads expired to
	// whoever reads it, and is not offered as waiting.
	expired := h.oneStep(t)
	requestID := h.askToWaive(t, boss, expired)
	h.letTheDeadlinePass(t, requestID)
	status, read, raw := h.readRequest(t, deputy, requestID)
	if status != http.StatusOK || read.Request.Status != "expired" || h.requestStatus(t, requestID) != "pending_approval" {
		t.Fatalf("a request past its deadline, not yet swept: %d (%s), want it read as expired while the table says %s",
			status, raw, h.requestStatus(t, requestID))
	}
	deadline := read.Request.ExpiresAt.UTC().Format(decidedOn)
	if _, waiting, raw := h.queue(t, deputy, ""); waiting.Total != 0 || len(waiting.Requests) != 0 {
		t.Fatalf("the queue offers a request past its deadline as waiting: %s", raw)
	}
	if _, overdue, raw := h.queue(t, deputy, "status=expired"); overdue.Total != 1 || len(overdue.Requests) != 1 ||
		overdue.Requests[0]["id"] != requestID || overdue.Requests[0]["status"] != "expired" {
		t.Fatalf("the requests listed as expired: %s, want the one past its deadline, read as expired", raw)
	}
	// The approval that finds it so records the expiry, and is refused.
	waited := h.everyRow(t)
	want := invalid("This request expired on " + deadline + " before anybody approved it, so nothing was applied. Ask again if it is still needed.")
	if status, _, raw := h.decide(t, deputy, requestID, "approve", decisionReason); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
		t.Fatalf("deputy's approval of an expired request: %d (%s), want 400 %s", status, raw, want)
	}
	h.requireOnlyTheClosing(t, waited, "an approval that found its request expired")
	if h.requestStatus(t, requestID) != "expired" || ledgerStatus(expired) != "expired" || !h.stepIsOpen(t, expired) {
		t.Fatalf("after the 400 the request is stored as %s and its ledger row as %s, want both expired and the step open",
			h.requestStatus(t, requestID), ledgerStatus(expired))
	}
	closed := h.everyRow(t)
	want = invalid("This request expired on " + deadline + ".")
	for _, verb := range []string{"approve", "reject"} {
		if status, _, raw := h.decide(t, deputy, requestID, verb, decisionReason); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
			t.Fatalf("deputy's %s of a request recorded as expired: %d (%s), want 400 %s", verb, status, raw, want)
		}
	}
	h.requireUnchanged(t, closed, "deciding a request recorded as expired")

	// Found past its deadline by a rejection: the same, in the rejection's words.
	late := h.oneStep(t)
	requestID = h.askToWaive(t, boss, late)
	h.letTheDeadlinePass(t, requestID)
	_, read, _ = h.readRequest(t, deputy, requestID)
	waited = h.everyRow(t)
	want = invalid("This request already expired on " + read.Request.ExpiresAt.UTC().Format(decidedOn) + ".")
	if status, _, raw := h.decide(t, deputy, requestID, "reject", decisionReason); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
		t.Fatalf("deputy's rejection of an expired request: %d (%s), want 400 %s", status, raw, want)
	}
	h.requireOnlyTheClosing(t, waited, "a rejection that found its request expired")
	if status, read, raw := h.readRequest(t, deputy, requestID); status != http.StatusOK || read.Request.Status != "expired" ||
		read.Request.DecidedBy != "" || h.requestStatus(t, requestID) != "expired" || ledgerStatus(late) != "expired" || !h.stepIsOpen(t, late) {
		t.Fatalf("after the 400 the request reads %d (%s), stored as %s; want it expired, decided by nobody, and the step open",
			status, raw, h.requestStatus(t, requestID))
	}

	// No longer true of the instance: its holder did the step while the
	// request waited. The approval records it as stale, and is refused.
	moved := h.start(t, twoSteps())
	requestID = h.askToWaive(t, boss, moved)
	h.completeStep(t, moved)
	waited = h.everyRow(t)
	want = invalid("This request no longer holds — the instance has moved since it was asked for — so nothing was applied. Preview again and ask afresh.")
	if status, _, raw := h.decide(t, deputy, requestID, "approve", decisionReason); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
		t.Fatalf("deputy's approval of a request the instance has left behind: %d (%s), want 400 %s", status, raw, want)
	}
	h.requireOnlyTheClosing(t, waited, "an approval that found its request stale")
	status, read, raw = h.readRequest(t, deputy, requestID)
	if status != http.StatusOK || read.Request.Status != "stale" || read.Request.DecidedBy != "" || read.Request.DecidedAt.IsZero() ||
		read.Request.Outcome["why"] != "the instance has moved since it was asked for" || h.requestStatus(t, requestID) != "stale" || ledgerStatus(moved) != "stale" {
		t.Fatalf("after the 400 the request reads %d (%s), stored as %s; want it stale, decided by nobody, and saying why",
			status, raw, h.requestStatus(t, requestID))
	}
	if h.openTasksOn(t, moved, "second") != 1 {
		t.Fatal("the instance is not at the step its holder moved it to")
	}
	closed = h.everyRow(t)
	want = invalid("This request went stale on " + read.Request.DecidedAt.UTC().Format(decidedOn) + ": what it asked for no longer held.")
	for _, verb := range []string{"approve", "reject"} {
		if status, _, raw := h.decide(t, deputy, requestID, verb, decisionReason); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
			t.Fatalf("deputy's %s of a stale request: %d (%s), want 400 %s", verb, status, raw, want)
		}
	}
	h.requireUnchanged(t, closed, "deciding a stale request")

	// Rejected: nobody decides it again.
	decided := h.oneStep(t)
	requestID = h.askToWaive(t, boss, decided)
	status, rejected, raw := h.decide(t, deputy, requestID, "reject", "not needed after all")
	if status != http.StatusOK || rejected.Request.Status != "rejected" {
		t.Fatalf("deputy rejects: %d (%s)", status, raw)
	}
	closed = h.everyRow(t)
	want = invalid("deputy rejected this on " + rejected.Request.DecidedAt.UTC().Format(decidedOn) + ".")
	for _, verb := range []string{"approve", "reject"} {
		if status, _, raw := h.decide(t, deputy, requestID, verb, decisionReason); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
			t.Fatalf("deputy's %s of a rejected request: %d (%s), want 400 %s", verb, status, raw, want)
		}
	}
	h.requireUnchanged(t, closed, "deciding a rejected request")
	if h.requestStatus(t, requestID) != "rejected" || !h.stepIsOpen(t, decided) {
		t.Fatal("a decided request changed, or its step was waived")
	}
}

// requireOnlyTheClosing fails the test unless exactly three tables differ from
// before: the request, its ledger row and the trail, which gained the entry
// that says why it was closed. A refusal that first records what it found
// writes those and nothing else — nothing of the instance, its tasks or
// anybody's inbox.
func (h *deviationHarness) requireOnlyTheClosing(t *testing.T, before map[string]string, after string) {
	t.Helper()
	now := h.everyRow(t)
	var changed []string
	for table, rows := range now {
		if before[table] != rows {
			changed = append(changed, table)
		}
	}
	slices.Sort(changed)
	if !slices.Equal(changed, []string{"audit_logs", "deviation_requests", "instance_deviations"}) {
		t.Fatalf("%s changed %v, want the request, its ledger row and one trail entry", after, changed)
	}
	var entries int
	if err := h.db.Raw(`SELECT count(*) FROM audit_logs`).Row().Scan(&entries); err != nil {
		t.Fatalf("count the trail: %v", err)
	}
	if was := strings.SplitN(before["audit_logs"], " ", 2)[0]; fmt.Sprint(entries-1) != was {
		t.Fatalf("%s left %d trail entries where there were %s, want one more", after, entries, was)
	}
}
