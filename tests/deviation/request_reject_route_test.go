package deviation_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// B3 over the API: whoever asked does not approve their own request, with a
// reason or without, and the refusal changes nothing. They may end it: a
// rejection by the requester is a withdrawal, and is not an approval by
// anybody.
func TestTheRequesterIsRefusedTheirOwnRequestOverTheAPIAndMayWithdrawIt(t *testing.T) {
	h := newDeviationRouteHarness(t)
	h.signIn(t, "alice", entities.RoleUser)
	instanceID := h.oneStep(t)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	requestID := h.askToWaive(t, boss, instanceID)
	before := h.everyRow(t)

	want := refusal("forbidden", "You asked for this. A different administrator has to approve it.")
	for _, reason := range []string{"", decisionReason} {
		if status, _, raw := h.decide(t, boss, requestID, "approve", reason); status != http.StatusForbidden || !sameJSON(t, raw, want) {
			t.Fatalf("boss approving his own request (reason %q): %d (%s), want 403 %s", reason, status, raw, want)
		}
		h.requireUnchanged(t, before, "the requester approving their own request")
	}
	if h.requestStatus(t, requestID) != "pending_approval" || !h.stepIsOpen(t, instanceID) {
		t.Fatal("a refused self-approval changed something")
	}

	status, withdrawn, raw := h.decide(t, boss, requestID, "reject", "asked for the wrong step")
	if status != http.StatusOK || withdrawn.Request.Status != "rejected" || withdrawn.Request.DecidedBy != "boss" || withdrawn.Request.SelfApproved ||
		withdrawn.Request.DecisionReason != "asked for the wrong step" || withdrawn.Applied {
		t.Fatalf("boss withdraws his own request: %d (%s), want it rejected by him and self-approved by nobody", status, raw)
	}
	if !h.stepIsOpen(t, instanceID) {
		t.Fatal("a withdrawal closed the step")
	}
	for who, account := range map[string]string{"the requester": h.accountID(t, "boss").String(), "the holder": h.accountID(t, "alice").String()} {
		if strings.Contains(raw, account) || strings.Contains(raw, "_by_id") {
			t.Errorf("the withdrawal carries the account id of %s: %s", who, raw)
		}
	}
	want = invalid("boss rejected this on " + withdrawn.Request.DecidedAt.UTC().Format(decidedOn) + ".")
	if status, _, raw := h.decide(t, deputy, requestID, "approve", ""); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
		t.Fatalf("deputy approving a withdrawn request: %d (%s), want 400 %s", status, raw, want)
	}
}

// A rejection says why, and the reason is kept. It ends the request and
// nothing else: the step is as it was, and the same waive can be asked for
// again — the earlier decision is history, not a lock (Review Focus 1). An
// approval's note is optional and kept as well; neither is longer than the
// record keeps.
func TestARejectionSaysWhyAndTheWaiveCanBeAskedAgain(t *testing.T) {
	h := newDeviationRouteHarness(t)
	h.signIn(t, "alice", entities.RoleUser)
	instanceID := h.oneStep(t)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	first := h.askToWaive(t, boss, instanceID)
	before := h.everyRow(t)

	sayWhy := invalid("Say why: a rejection keeps its reason with the record.")
	for name, body := range map[string]string{"no body": "", "an empty object": "{}", "a reason of spaces": `{"reason":"   "}`, "a reason of null": `{"reason":null}`} {
		if status, raw := h.send(t, deputy, requestPath(first)+"/reject", body); status != http.StatusBadRequest || !sameJSON(t, raw, sayWhy) {
			t.Fatalf("a rejection with %s: %d (%s), want 400 %s", name, status, raw, sayWhy)
		}
	}
	tooLong := invalid("The reason is longer than 2000 characters; say it more briefly.")
	for _, verb := range []string{"approve", "reject"} {
		if status, _, raw := h.decide(t, deputy, first, verb, strings.Repeat("y", 2001)); status != http.StatusBadRequest || !sameJSON(t, raw, tooLong) {
			t.Fatalf("a %s with a reason of 2001 characters: %d (%s), want 400 %s", verb, status, raw, tooLong)
		}
	}
	h.requireUnchanged(t, before, "decisions with no reason, or too long a one")

	status, rejected, raw := h.decide(t, deputy, first, "reject", "  the director did not agree  ")
	if status != http.StatusOK || rejected.Request.ID != first || rejected.Request.Status != "rejected" || rejected.Request.DecidedBy != "deputy" ||
		rejected.Request.DecisionReason != "the director did not agree" || rejected.Request.SelfApproved {
		t.Fatalf("deputy rejects: %d (%s)", status, raw)
	}
	written := object(t, raw)
	requireFields(t, written, "a rejection", "request")
	requireFields(t, written["request"], "a rejected request",
		append([]string{"instance_id", "decided_by", "decided_at", "decision_reason"}, requestFields...)...)
	if nulls := nullsIn(written, "reply"); len(nulls) != 0 {
		t.Fatalf("the rejection holds null at %v: %s", nulls, raw)
	}
	for _, account := range []string{"requested_by_id", "decided_by_id", h.accountID(t, "boss").String(), h.accountID(t, "deputy").String(),
		h.accountID(t, "alice").String()} {
		if strings.Contains(raw, account) {
			t.Fatalf("the rejection carries an account id (%s): %s", account, raw)
		}
	}
	_, ledger, ledgerRaw := h.readDeviations(t, deputy, instanceID.String())
	if len(ledger.Deviations) != 1 || ledger.Deviations[0]["status"] != "rejected" || !h.stepIsOpen(t, instanceID) {
		t.Fatalf("after a rejection the ledger reads %s; want its one row rejected and the step open", ledgerRaw)
	}

	// Asked again: a fresh request, and this one is approved, with a note.
	second := h.askToWaive(t, boss, instanceID)
	if second == first {
		t.Fatalf("asking again after a rejection answered with the rejected request %s", first)
	}
	status, approved, raw := h.decide(t, deputy, second, "approve", "  "+decisionReason+"  ")
	if status != http.StatusOK || !approved.Applied || approved.Request.DecisionReason != decisionReason {
		t.Fatalf("deputy approves the second request with a note: %d (%s)", status, raw)
	}
	requireFields(t, object(t, raw)["request"], "a request approved with a note",
		append([]string{"instance_id", "decided_by", "decided_at", "decision_reason"}, requestFields...)...)
	if h.stepIsOpen(t, instanceID) {
		t.Fatal("the approved waive left the step open")
	}
}
