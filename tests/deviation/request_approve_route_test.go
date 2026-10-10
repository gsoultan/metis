package deviation_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// B1, B3 over the API: a second administrator reads what was asked, approves
// it, and the step moves on.
func TestASecondAdministratorApprovesAWaiveOverTheAPI(t *testing.T) {
	h := newDeviationHarness(t)
	// The step's holder is an account too: hers is an id a reply could carry.
	h.signIn(t, "alice", entities.RoleUser)
	instanceID := h.oneStep(t)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	requestID := h.askToWaive(t, boss, instanceID)

	status, read, raw := h.readRequest(t, deputy, requestID)
	if status != http.StatusOK || read.Request.ID != requestID || read.Request.Kind != "instance_waive" || read.Request.Status != "pending_approval" ||
		read.Request.RequestedBy != "boss" || read.Request.Plan["node_id"] != "step" {
		t.Fatalf("deputy reads the request: %d (%s)", status, raw)
	}
	if len(read.Request.Because) != 1 || read.Request.Because[0] != "“Approve” would be waived: nobody performs it, and the process moves on" {
		t.Fatalf("why it needs a second administrator: %q", read.Request.Because)
	}
	// What the second administrator reads is what the requester's preview
	// showed, field for field, with the counts beside what it lists — and why
	// it needs them.
	requireFields(t, read.Request.Plan, "the plan of the request", append([]string{"because"}, planFields...)...)
	if read.Request.Plan["open_work_in_all"] != float64(1) || read.Request.Plan["requires_second_approver"] != true ||
		len(read.Request.Plan["open_work"].([]any)) != 1 {
		t.Fatalf("the plan of the request: %s", raw)
	}
	// What was asked is the sealed command: the approval applies it, and
	// nothing an approver sends.
	requireFields(t, read.Request.Command, "the command of the request", "instance_id", "kind", "node_id", "reason", "visit_key")
	if read.Request.Command["kind"] != "waive" || read.Request.Command["node_id"] != "step" || read.Request.Command["reason"] != routeReason ||
		read.Request.Command["instance_id"] != instanceID.String() {
		t.Fatalf("the command of the request: %s", raw)
	}
	written := object(t, raw)
	requireFields(t, written, "the read of a request", "request")
	requireFields(t, written["request"], "a waiting request", append([]string{"instance_id"}, requestFields...)...)
	// A waive is for one instance, named by instance_id; the list of instances
	// is a migration's, and a waive's is the empty list it is.
	if got, _ := json.Marshal(written["request"].(map[string]any)["instances"]); string(got) != "[]" || read.Request.InstancesInAll != 0 {
		t.Fatalf("a waive lists instances %s of %d, want none of none", got, read.Request.InstancesInAll)
	}
	if nulls := nullsIn(written, "reply"); len(nulls) != 0 {
		t.Fatalf("the request holds null at %v: %s", nulls, raw)
	}
	if odd := notSnakeCase(t, map[string]any{"request": written["request"]}); len(odd) != 0 {
		t.Errorf("fields not written as the other routes write theirs: %v", odd)
	}
	accounts := []string{"requested_by_id", "decided_by_id", "approved_by_id", "actor_id", "assignee_id",
		h.accountID(t, "boss").String(), h.accountID(t, "deputy").String(), h.accountID(t, "alice").String()}
	for _, account := range accounts {
		if strings.Contains(raw, account) {
			t.Fatalf("the reply carries an account id (%s): %s", account, raw)
		}
	}
	// The holder is named, as the preview named her: by name.
	if work := read.Request.Plan["open_work"].([]any); work[0].(map[string]any)["assignee"] != "alice" {
		t.Fatalf("the plan of the request does not name the step's holder: %s", raw)
	}

	status, approved, raw := h.decide(t, deputy, requestID, "approve", "")
	if status != http.StatusOK || !approved.Applied || approved.Request.Status != "applied" || approved.Request.DecidedBy != "deputy" ||
		approved.Request.SelfApproved || approved.Deviation["approved_by"] != "deputy" || approved.Deviation["status"] != "applied" {
		t.Fatalf("deputy approves: %d (%s)", status, raw)
	}
	written = object(t, raw)
	requireFields(t, written, "an approval", "request", "applied", "deviation", "plan")
	requireFields(t, written["request"], "a decided request", append([]string{"instance_id", "decided_by", "decided_at"}, requestFields...)...)
	requireFields(t, written["plan"], "the plan it was applied with", planFields...)
	if nulls := nullsIn(written, "reply"); len(nulls) != 0 {
		t.Fatalf("the approval holds null at %v: %s", nulls, raw)
	}
	for _, account := range accounts {
		if strings.Contains(raw, account) {
			t.Fatalf("the approval carries an account id (%s): %s", account, raw)
		}
	}
	if h.stepIsOpen(t, instanceID) {
		t.Fatal("the approved waive left the step open")
	}
	// Read again, it says what became of it.
	if status, read, raw := h.readRequest(t, boss, requestID); status != http.StatusOK || read.Request.Status != "applied" ||
		read.Request.DecidedBy != "deputy" || read.Request.Outcome["deviation_id"] != approved.Deviation["id"] {
		t.Fatalf("the request read after its approval: %d (%s)", status, raw)
	}
	before := h.everyRow(t)
	want := invalid(fmt.Sprintf("deputy approved this on %s, and it was applied.", approved.Request.DecidedAt.UTC().Format(decidedOn)))
	for _, verb := range []string{"approve", "reject"} {
		if status, _, raw := h.decide(t, deputy, requestID, verb, decisionReason); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
			t.Fatalf("deputy's %s of a request already approved: %d (%s), want 400 %s", verb, status, raw, want)
		}
	}
	h.requireUnchanged(t, before, "deciding a request already approved")
}

// D9 names a waive, and a migration that loosens a rule. Cancelling an
// instance and holding it for somebody to decide stay one administrator's
// call: each is applied by the request that asks for it, a 200, and no
// request for a second administrator is made.
func TestACancelAndAHoldStillApplyOnOneAdministratorsCall(t *testing.T) {
	h := newDeviationRouteHarness(t)
	boss := h.signIn(t, "boss", entities.RoleAdmin)
	for _, kind := range []string{"hold", "cancel"} {
		instanceID := h.oneStep(t)
		apply, planned := h.previewed(t, boss, instanceID, map[string]any{"kind": kind, "node_id": "step", "reason": routeReason})
		if planned.Plan.RequiresSecondApprover {
			t.Fatalf("the plan of a %s says it needs a second administrator", kind)
		}
		reply, raw := h.deviateObject(t, boss, instanceID, apply)
		requireFields(t, reply, "the apply of a "+kind, "plan", "applied", "replayed", "deviation")
		record, _ := reply["deviation"].(map[string]any)
		if reply["applied"] != true || record["status"] != "applied" || record["actor"] != "boss" {
			t.Fatalf("a %s by one administrator: %s, want it applied", kind, raw)
		}
		if _, waits := record["request_id"]; waits || strings.Contains(raw, "approved_by") || strings.Contains(raw, "pending_approval") {
			t.Fatalf("a %s names a request or an approver: %s", kind, raw)
		}
	}
	if n := h.requestCount(t); n != 0 {
		t.Fatalf("a hold and a cancel made %d request(s) for a second administrator", n)
	}
	if status, queue, raw := h.queue(t, boss, ""); status != http.StatusOK || queue.Total != 0 || !sameJSON(t, raw, `{"requests":[],"total":0}`) {
		t.Fatalf("the queue after a hold and a cancel: %d (%s), want it empty and its list a list", status, raw)
	}
}

// A retry answers with the same request — a 202 again, because nothing has
// been done yet — and another administrator asking for the same thing is
// pointed at the waiting one. A preview says so before an apply would.
func TestAWaiveAskedTwiceAnswersWithTheSameRequest(t *testing.T) {
	h := newDeviationRouteHarness(t)
	h.signIn(t, "alice", entities.RoleUser)
	instanceID := h.oneStep(t)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	apply, _ := h.previewed(t, boss, instanceID, map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason})
	first := h.askToWaive(t, boss, instanceID)
	before := h.everyRow(t)

	// The requester's retry: the request that waits, said to be a replay.
	status, again, raw := h.deviate(t, boss, instanceID, apply)
	if status != http.StatusAccepted || again.Applied || !again.Replayed || again.PendingApproval == nil ||
		again.PendingApproval.RequestID != first || again.PendingApproval.Status != "pending_approval" || again.PendingApproval.RequestedBy != "boss" {
		t.Fatalf("the same waive asked again by whoever asked: %d (%s), want a 202 replay naming request %s", status, raw, first)
	}
	written := object(t, raw)
	requireFields(t, written, "a request asked again", "plan", "applied", "replayed", "deviation", "pending_approval")
	if record := written["deviation"].(map[string]any); record["status"] != "pending_approval" || record["request_id"] != first {
		t.Fatalf("the record of a waive that still waits: %s", raw)
	}
	if nulls := nullsIn(written, "reply"); len(nulls) != 0 {
		t.Errorf("the replayed 202 holds null at %v: %s", nulls, raw)
	}
	for who, account := range map[string]string{"the requester": h.accountID(t, "boss").String(), "the holder": h.accountID(t, "alice").String()} {
		if strings.Contains(raw, account) {
			t.Errorf("the replayed 202 carries the account id of %s: %s", who, raw)
		}
	}
	if again := h.askToWaive(t, boss, instanceID); again != first {
		t.Fatalf("a retry made request %s beside %s", again, first)
	}

	// The requester's preview of the same thing warns that it already waits,
	// and still applies; anybody else's refuses, in the apply's words.
	waiting := fmt.Sprintf("A request to waive “Approve” is already waiting for approval (request %s, asked by boss", first)
	status, mine, raw := h.deviate(t, boss, instanceID, map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason})
	if status != http.StatusOK || !mine.Plan.Applicable || len(mine.Plan.Warnings) != 2 {
		t.Fatalf("the requester's preview of a waive that waits: %d (%s), want a plan that applies with the warning it had and one of the request", status, raw)
	}
	wantWarning := waiting + ", until " + again.PendingApproval.ExpiresAt.UTC().Format(decidedOn) +
		"). Applying this again answers with that request and makes no second one."
	if mine.Plan.Warnings[1] != wantWarning {
		t.Fatalf("the requester's preview warns %q, want %q", mine.Plan.Warnings[1], wantWarning)
	}
	pointed := waiting + "); approve or reject that one."
	status, theirs, raw := h.deviate(t, deputy, instanceID, map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason})
	if status != http.StatusOK || theirs.Plan.Applicable || len(theirs.Plan.Refusals) != 1 || theirs.Plan.Refusals[0] != pointed {
		t.Fatalf("deputy previews: %d (%s), want a plan refusing with %q", status, raw, pointed)
	}
	status, _, raw = h.deviate(t, deputy, instanceID, map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason,
		"visit_key": theirs.Plan.VisitKey, "dry_run": false})
	if status != http.StatusBadRequest || !sameJSON(t, raw, invalid(pointed)) {
		t.Fatalf("deputy asking for the same waive: %d (%s), want 400 %s", status, raw, invalid(pointed))
	}
	// The requester asking for something else of the visit is pointed at it too.
	other := map[string]any{"kind": "waive", "node_id": "step", "reason": "another reason", "visit_key": theirs.Plan.VisitKey, "dry_run": false}
	if status, _, raw := h.deviate(t, boss, instanceID, other); status != http.StatusBadRequest || !sameJSON(t, raw, invalid(pointed)) {
		t.Fatalf("the requester asking for another waive of the visit: %d (%s), want 400 %s", status, raw, invalid(pointed))
	}
	h.requireUnchanged(t, before, "asking again for a waive that waits")
	if h.requestCount(t) != 1 {
		t.Fatalf("%d requests after one waive asked for several times, want one", h.requestCount(t))
	}
}

// A decision is safe to retry. Under an Idempotency-Key the header's own
// check answers the retry with the first answer, whole; without one the
// second approval meets a request already decided, and is told who decided
// it. Either way the waive is made once. Asking is the same: the 202 is kept
// under its key and returned as a 202.
func TestAnApprovalSentAgainIsAnsweredOnce(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.oneStep(t)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	apply, _ := h.previewed(t, boss, instanceID, map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason})
	applyBody := fmt.Sprintf(`{"kind":"waive","node_id":"step","reason":%q,"visit_key":%q,"dry_run":false}`, routeReason, apply["visit_key"])

	// Asked under a key, twice: the first 202, returned.
	status, asked, replayed := h.call(t, http.MethodPost, boss, deviationsPath(instanceID), applyBody, "Idempotency-Key", "ask")
	if status != http.StatusAccepted || replayed || !strings.Contains(asked, `"replayed":false`) {
		t.Fatalf("a waive asked for under a key: %d, replayed by the header %v (%s)", status, replayed, asked)
	}
	status, again, replayed := h.call(t, http.MethodPost, boss, deviationsPath(instanceID), applyBody, "Idempotency-Key", "ask")
	if status != http.StatusAccepted || !replayed || again != asked {
		t.Fatalf("the same ask under the same key: %d, replayed by the header %v\n%s\nwant the first answer again\n%s", status, replayed, again, asked)
	}
	var first deviateReply
	if err := json.Unmarshal([]byte(asked), &first); err != nil || first.PendingApproval == nil {
		t.Fatalf("decode the 202: %v (%s)", err, asked)
	}
	approve := requestPath(first.PendingApproval.RequestID) + "/approve"

	status, approved, replayed := h.call(t, http.MethodPost, deputy, approve, `{}`, "Idempotency-Key", "approve")
	if status != http.StatusOK || replayed || !strings.Contains(approved, `"applied":true`) {
		t.Fatalf("an approval under a key: %d, replayed by the header %v (%s)", status, replayed, approved)
	}
	done := h.everyRow(t)
	status, repeated, replayed := h.call(t, http.MethodPost, deputy, approve, `{}`, "Idempotency-Key", "approve")
	if status != http.StatusOK || !replayed || repeated != approved {
		t.Fatalf("the same approval under the same key: %d, replayed by the header %v\n%s\nwant the first answer again\n%s", status, replayed, repeated, approved)
	}
	// The same key with another body is refused by the header's check, in
	// plain text, before the route sees it.
	if status, raw, _ := h.call(t, http.MethodPost, deputy, approve, `{"reason":"another"}`, "Idempotency-Key", "approve"); status != http.StatusConflict || strings.HasPrefix(raw, "{") {
		t.Fatalf("another approval under the first one's key: %d (%s), want the header's own 409, in plain text", status, raw)
	}
	// With no key, and under a new one, the route itself answers: the request
	// has been decided.
	var reply requestReply
	if err := json.Unmarshal([]byte(approved), &reply); err != nil {
		t.Fatalf("decode the approval: %v (%s)", err, approved)
	}
	want := invalid(fmt.Sprintf("deputy approved this on %s, and it was applied.", reply.Request.DecidedAt.UTC().Format(decidedOn)))
	if status, raw, _ := h.call(t, http.MethodPost, deputy, approve, `{}`); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
		t.Fatalf("the same approval with no key: %d (%s), want 400 %s", status, raw, want)
	}
	if status, raw, replayed := h.call(t, http.MethodPost, deputy, approve, `{}`, "Idempotency-Key", "a-new-key"); status != http.StatusBadRequest || replayed || !sameJSON(t, raw, want) {
		t.Fatalf("the same approval under a new key: %d, replayed by the header %v (%s), want the route's own 400 %s", status, replayed, raw, want)
	}
	// The idempotency store keeps each first answer, and nothing else changed.
	for table, rows := range h.everyRow(t) {
		if done[table] != rows && table != "idempotency_records" {
			t.Errorf("sending the approval again changed %s (%s, was %s)", table, rows, done[table])
		}
	}
	if rows := h.rowCount(t, instanceID); rows != 1 || h.requestCount(t) != 1 {
		t.Fatalf("the ledger holds %d rows and %d request(s) after one waive asked for twice and approved four times, want one of each", rows, h.requestCount(t))
	}
}

// A refusal that first records what it found is safe to retry as well. An
// approval that finds its request expired closes it and answers 400; sent
// again under the same Idempotency-Key, that 400 is returned whole — the
// same bytes, the header saying it is a repeat — and nothing is written a
// second time. Under no key the route answers for itself: the request is
// recorded as expired, in the words that says so, and nothing is written
// either.
func TestARefusalThatRecordedSomethingIsAnsweredOnceUnderAKey(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.oneStep(t)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	requestID := h.askToWaive(t, boss, instanceID)
	h.letTheDeadlinePass(t, requestID)
	_, read, _ := h.readRequest(t, deputy, requestID)
	deadline := read.Request.ExpiresAt.UTC().Format(decidedOn)
	approve := requestPath(requestID) + "/approve"
	waited := h.everyRow(t)

	want := invalid("This request expired on " + deadline + " before anybody approved it, so nothing was applied. Ask again if it is still needed.")
	status, first, replayed := h.call(t, http.MethodPost, deputy, approve, `{}`, "Idempotency-Key", "late")
	if status != http.StatusBadRequest || replayed || !sameJSON(t, first, want) {
		t.Fatalf("an approval of an expired request, under a key: %d, replayed by the header %v (%s), want 400 %s", status, replayed, first, want)
	}
	if h.requestStatus(t, requestID) != "expired" {
		t.Fatalf("after the 400 the request is stored as %s, want expired", h.requestStatus(t, requestID))
	}
	// The closing, and the answer the header's check keeps.
	closed := h.everyRow(t)
	var changed []string
	for table, rows := range closed {
		if waited[table] != rows {
			changed = append(changed, table)
		}
	}
	slices.Sort(changed)
	if !slices.Equal(changed, []string{"audit_logs", "deviation_requests", "idempotency_records", "instance_deviations"}) {
		t.Fatalf("the approval that found its request expired changed %v, want the closing and the kept answer", changed)
	}

	status, again, replayed := h.call(t, http.MethodPost, deputy, approve, `{}`, "Idempotency-Key", "late")
	if status != http.StatusBadRequest || !replayed || again != first {
		t.Fatalf("the same approval under the same key: %d, replayed by the header %v\n%s\nwant the first answer again\n%s", status, replayed, again, first)
	}
	h.requireUnchanged(t, closed, "the same approval under the same key")

	recorded := invalid("This request expired on " + deadline + ".")
	if status, raw, _ := h.call(t, http.MethodPost, deputy, approve, `{}`); status != http.StatusBadRequest || !sameJSON(t, raw, recorded) {
		t.Fatalf("the same approval with no key: %d (%s), want 400 %s", status, raw, recorded)
	}
	h.requireUnchanged(t, closed, "the same approval with no key")
	if !h.stepIsOpen(t, instanceID) {
		t.Fatal("an approval of an expired request closed the step")
	}
}
