package deviation_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
)

// The queue is what an administrator can act on: what still waits, newest
// first, a page at a time, with how many there are in all. A listed request
// is not a whole one. The queue does not read what was asked, what the
// requester was shown or which instances it covers, so it does not write
// them — the fields are absent, not null and not empty, which would read as
// "asked for nothing" — and the single read has them.
func TestARequestIsListedWithoutWhatOnlyTheSingleReadHas(t *testing.T) {
	h := newDeviationRouteHarness(t)
	h.signIn(t, "alice", entities.RoleUser)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	older, newer, turnedDown, overdue := h.oneStep(t), h.oneStep(t), h.oneStep(t), h.oneStep(t)
	olderID, newerID := h.askToWaive(t, boss, older), h.askToWaive(t, boss, newer)
	rejectedID, overdueID := h.askToWaive(t, boss, turnedDown), h.askToWaive(t, boss, overdue)
	if status, _, raw := h.decide(t, deputy, rejectedID, "reject", "not needed after all"); status != http.StatusOK {
		t.Fatalf("deputy rejects: %d (%s)", status, raw)
	}
	h.letTheDeadlinePass(t, overdueID)
	before := h.everyRow(t)
	ids := func(page queueReply) []string {
		listed := make([]string, 0, len(page.Requests))
		for _, request := range page.Requests {
			listed = append(listed, fmt.Sprint(request["id"]))
		}
		return listed
	}
	accounts := []string{"requested_by_id", "decided_by_id", h.accountID(t, "boss").String(), h.accountID(t, "deputy").String(),
		h.accountID(t, "alice").String()}

	// With nothing said it lists what still waits, newest first.
	status, waiting, raw := h.queue(t, deputy, "")
	if status != http.StatusOK || waiting.Total != 2 || strings.Join(ids(waiting), ",") != newerID+","+olderID {
		t.Fatalf("the queue: %d (%s), want the two that wait, newest first", status, raw)
	}
	requireFields(t, object(t, raw), "the queue", "requests", "total")
	for _, listed := range waiting.Requests {
		requireFields(t, listed, "a listed request", append([]string{"instance_id"}, listedRequestFields...)...)
		if listed["kind"] != "instance_waive" || listed["status"] != "pending_approval" || listed["requested_by"] != "boss" ||
			listed["reason"] != routeReason || listed["self_approved"] != false {
			t.Fatalf("a listed request: %v", listed)
		}
	}
	if nulls := nullsIn(object(t, raw), "reply"); len(nulls) != 0 {
		t.Fatalf("the queue holds null at %v: %s", nulls, raw)
	}
	for _, heavy := range []string{`"command"`, `"plan"`, `"instances"`, `"instances_in_all"`, `"because"`, `"visit_key"`, `"open_work"`} {
		if strings.Contains(raw, heavy) {
			t.Fatalf("the queue writes %s, which it does not read: %s", heavy, raw)
		}
	}
	for _, account := range accounts {
		if strings.Contains(raw, account) {
			t.Fatalf("the queue carries an account id (%s): %s", account, raw)
		}
	}
	// The single read of one of them has what the queue left out.
	_, whole, wholeRaw := h.readRequest(t, deputy, newerID)
	requireFields(t, object(t, wholeRaw)["request"], "the same request, read by itself", append([]string{"instance_id"}, requestFields...)...)
	if whole.Request.Command["node_id"] != "step" || whole.Request.Plan["node_id"] != "step" || len(whole.Request.Because) != 1 {
		t.Fatalf("the single read: %s", wholeRaw)
	}

	// By status, as each request reads now.
	if _, same, raw := h.queue(t, deputy, "status=pending_approval"); strings.Join(ids(same), ",") != newerID+","+olderID || same.Total != 2 {
		t.Fatalf("listed as pending_approval: %s", raw)
	}
	_, rejected, raw := h.queue(t, deputy, "status=rejected")
	if rejected.Total != 1 || strings.Join(ids(rejected), ",") != rejectedID {
		t.Fatalf("listed as rejected: %s", raw)
	}
	requireFields(t, rejected.Requests[0], "a listed request somebody rejected",
		append([]string{"instance_id", "decided_by", "decided_at", "decision_reason"}, listedRequestFields...)...)
	if rejected.Requests[0]["decided_by"] != "deputy" || rejected.Requests[0]["decision_reason"] != "not needed after all" {
		t.Fatalf("a listed request somebody rejected: %s", raw)
	}
	if _, expired, raw := h.queue(t, deputy, "status=expired"); expired.Total != 1 || strings.Join(ids(expired), ",") != overdueID ||
		expired.Requests[0]["status"] != "expired" {
		t.Fatalf("listed as expired: %s, want the one past its deadline, read as expired before any sweep", raw)
	}
	for _, none := range []string{"status=applied", "status=stale", "status=approved", "status=interrupted", "project_id=" + uuid.Must(uuid.NewV7()).String()} {
		if status, empty, raw := h.queue(t, deputy, none); status != http.StatusOK || !sameJSON(t, raw, `{"requests":[],"total":0}`) || empty.Total != 0 {
			t.Fatalf("the queue with %s: %d (%s), want it empty and its list a list", none, status, raw)
		}
	}
	if _, mine, raw := h.queue(t, deputy, "project_id="+h.projID.String()); mine.Total != 2 || strings.Join(ids(mine), ",") != newerID+","+olderID {
		t.Fatalf("the queue of the project: %s", raw)
	}

	// A page at a time: what is listed, beside how many there are.
	_, firstPage, raw := h.queue(t, deputy, "page_size=1")
	if firstPage.Total != 2 || strings.Join(ids(firstPage), ",") != newerID {
		t.Fatalf("the first page of one: %s, want the newest of two", raw)
	}
	if _, secondPage, raw := h.queue(t, deputy, "page_size=1&page=2"); secondPage.Total != 2 || strings.Join(ids(secondPage), ",") != olderID {
		t.Fatalf("the second page of one: %s, want the older of two", raw)
	}
	if _, past, raw := h.queue(t, deputy, "page_size=1&page=3"); past.Total != 2 || !strings.Contains(raw, `"requests":[]`) {
		t.Fatalf("a page past the last: %s, want an empty list beside the count", raw)
	}

	// A page before the first is the first, and a size of nothing the
	// server's own: the page is bounded whatever is asked for.
	for _, query := range []string{"page=0", "page=-3", "page_size=0", "page_size=-1", "page_size=100000", "page_size=1000000", "page=&page_size="} {
		if status, bounded, raw := h.queue(t, deputy, query); status != http.StatusOK || bounded.Total != 2 || strings.Join(ids(bounded), ",") != newerID+","+olderID {
			t.Errorf("the queue with %s: %d (%s), want the first page", query, status, raw)
		}
	}

	// What cannot be asked for is the caller's to fix.
	for query, sentence := range map[string]string{
		"status=waiting": "status must be one of pending_approval, approved, applied, interrupted, stale, rejected, expired",
		"status=PENDING": "status must be one of pending_approval, approved, applied, interrupted, stale, rejected, expired",
		"page=two":       "page and page_size are whole numbers",
		"page_size=1.5":  "page and page_size are whole numbers",
		// A page so far on that where it starts no longer fits in a number
		// would be read as the first; it is refused instead.
		"page=9223372036854775807": "page and page_size are at most 1000000",
		"page_size=1000001":        "page and page_size are at most 1000000",
		"project_id=not-an-id":     `project id "not-an-id" is not a valid identifier`,
	} {
		if status, _, raw := h.queue(t, deputy, query); status != http.StatusBadRequest || !sameJSON(t, raw, invalid(sentence)) {
			t.Errorf("the queue with %s: %d (%s), want 400 %s", query, status, raw, invalid(sentence))
		}
	}
	h.requireUnchanged(t, before, "reading the queue")
}
