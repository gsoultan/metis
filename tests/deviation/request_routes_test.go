package deviation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	httptransport "github.com/go-kit/kit/transport/http"

	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/endpoints/deviation"
	"github.com/gsoultan/metis/server/transports/https/common"
	"github.com/gsoultan/metis/server/transports/https/deviations"
)

// object decodes a reply into the fields it is written with.
func object(t *testing.T, raw string) map[string]any {
	t.Helper()
	var written map[string]any
	if err := json.Unmarshal([]byte(raw), &written); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	return written
}

// oneStep starts an instance that waits at "step", a task alice holds.
func (h *deviationHarness) oneStep(t *testing.T) uuid.UUID {
	t.Helper()
	return h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
}

// Design §8: the four request routes are an administrator's of the request's
// organization. Everybody else is refused, on every one of them, and after
// each refusal no table holds anything it did not hold before. Another
// organization's administrator is told there is no such request — in the
// words an administrator of this one is told of a request that does not
// exist — and sees an empty queue.
func TestOnlyAnOrganizationsAdministratorsSeeAndDecideItsRequests(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.oneStep(t)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	// Everybody signs in before anything is compared: signing in is not what
	// is being asked about.
	type caller struct {
		name, token string
		headers     []string
		status      int
		reply       string
		// seesAnEmptyQueue is an administrator of another organization: the
		// queue is theirs to read, and holds nothing of this one.
		seesAnEmptyQueue bool
	}
	forbidden := refusal("forbidden", needsTheAdministratorRole)
	notFound := refusal("not found", noSuchRequest)
	callers := []caller{}
	for _, role := range []string{entities.RoleUser, entities.RoleOperator, entities.RoleDesigner} {
		callers = append(callers, caller{name: "a " + role, token: h.signIn(t, "refused-"+strings.ToLower(role), role),
			status: http.StatusForbidden, reply: forbidden})
	}
	both, elsewhere := h.signInToBoth(t, "boss-elsewhere")
	outsider := h.signInElsewhere(t, "outsider-boss", entities.RoleAdmin)
	callers = append(callers,
		caller{name: "an administrator of another organization, acting in this one", token: both,
			headers: []string{"X-Organization-ID", h.orgID.String()}, status: http.StatusForbidden, reply: forbidden},
		caller{name: "the same account, acting in its own organization", token: both,
			headers: []string{"X-Organization-ID", elsewhere.String()}, status: http.StatusNotFound, reply: notFound, seesAnEmptyQueue: true},
		caller{name: "another organization's administrator", token: outsider,
			status: http.StatusNotFound, reply: notFound, seesAnEmptyQueue: true},
		// Naming an organization in the header does not put an account in it:
		// the header chooses among the caller's own.
		caller{name: "another organization's administrator, naming this organization in the header", token: outsider,
			headers: []string{"X-Organization-ID", h.orgID.String()}, status: http.StatusUnauthorized},
		caller{name: "nobody", status: http.StatusUnauthorized},
	)
	requestID := h.askToWaive(t, boss, instanceID)
	decision := fmt.Sprintf(`{"reason":%q}`, decisionReason)
	routes := []struct{ name, method, path, body string }{
		{"the queue", http.MethodGet, requestsPath, ""},
		{"the read", http.MethodGet, requestPath(requestID), ""},
		{"the approval", http.MethodPost, requestPath(requestID) + "/approve", decision},
		{"the rejection", http.MethodPost, requestPath(requestID) + "/reject", decision},
	}
	before := h.everyRow(t)

	for _, who := range callers {
		for _, route := range routes {
			status, raw, _ := h.call(t, route.method, who.token, route.path, route.body, who.headers...)
			wantStatus, wantReply := who.status, who.reply
			if who.seesAnEmptyQueue && route.path == requestsPath {
				wantStatus, wantReply = http.StatusOK, `{"requests":[],"total":0}`
			}
			if status != wantStatus {
				t.Fatalf("%s on %s: %d (%s), want %d", who.name, route.name, status, raw, wantStatus)
			}
			if wantReply != "" && !sameJSON(t, raw, wantReply) {
				t.Fatalf("%s on %s was told %s, want %s", who.name, route.name, raw, wantReply)
			}
			for _, told := range []string{"Approve", "alice", "boss\"", requestID, instanceID.String(), routeReason} {
				if strings.Contains(raw, told) {
					t.Fatalf("%s on %s was told something of the request (%s): %s", who.name, route.name, told, raw)
				}
			}
			h.requireUnchanged(t, before, who.name+" on "+route.name)
		}
	}

	// A request that is not there, asked for by an administrator of this
	// organization, is answered in the same words, byte for byte: the answer
	// to another organization's administrator does not say which it was.
	nowhere := uuid.Must(uuid.NewV7()).String()
	for _, route := range []struct{ name, method, suffix, body string }{
		{"the read", http.MethodGet, "", ""},
		{"the approval", http.MethodPost, "/approve", decision},
		{"the rejection", http.MethodPost, "/reject", decision},
	} {
		missingStatus, missing, _ := h.call(t, route.method, deputy, requestPath(nowhere)+route.suffix, route.body)
		foreignStatus, foreign, _ := h.call(t, route.method, outsider, requestPath(requestID)+route.suffix, route.body)
		if missingStatus != http.StatusNotFound || foreignStatus != http.StatusNotFound || !sameJSON(t, missing, notFound) || missing != foreign {
			t.Fatalf("%s: a request that is not there %d (%s), another organization's %d (%s); want 404 %s for each",
				route.name, missingStatus, missing, foreignStatus, foreign, notFound)
		}
	}
	h.requireUnchanged(t, before, "requests for a request the caller cannot see")
	if h.requestStatus(t, requestID) != "pending_approval" || !h.stepIsOpen(t, instanceID) {
		t.Fatal("refused calls changed the request or the instance")
	}

	// And the comparison every refusal above passed does see a decision: an
	// administrator of the organization is not refused, and the tables an
	// approval writes to are not what they were.
	if status, approved, raw := h.decide(t, deputy, requestID, "approve", decisionReason); status != http.StatusOK || !approved.Applied {
		t.Fatalf("the organization's second administrator approves: %d (%s)", status, raw)
	}
	after := h.everyRow(t)
	for _, table := range []string{"deviation_requests", "instance_deviations", "tasks", "process_instances", "audit_logs"} {
		if after[table] == before[table] {
			t.Errorf("the approval left %s as it was (%s): the comparison would not have seen a refused request acting", table, after[table])
		}
	}
}

// B1, B3 over the API: a second administrator reads what was asked, approves
// it, and the step moves on.
func TestASecondAdministratorApprovesAWaiveOverTheAPI(t *testing.T) {
	h := newDeviationHarness(t)
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
	accounts := []string{"requested_by_id", "decided_by_id", "approved_by_id", "actor_id", h.accountID(t, "boss").String(), h.accountID(t, "deputy").String()}
	for _, account := range accounts {
		if strings.Contains(raw, account) {
			t.Fatalf("the reply carries an account id (%s): %s", account, raw)
		}
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

// decisionCouldNotBeRead is what a decision the server cannot read is answered.
const decisionCouldNotBeRead = "this request could not be read: send one JSON object with a reason, or nothing; name the field exactly and once"

// A decision the server cannot read is refused in plain words (slice 2's I-3),
// over the route, whatever the request it names: the body is read first, as
// on every route, and the refusal says nothing of any request.
//
// A decision says a reason and nothing else. What is approved is the command
// the request sealed when it was asked for: an approval that sends a command,
// outputs or a visit key of its own is refused — never read, and never
// ignored.
func TestADecisionThatCannotBeReadIsRefusedInPlainWords(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.start(t, orderBySize())
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	member := h.signIn(t, "member", entities.RoleUser)
	apply, _ := h.previewed(t, boss, instanceID, map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason,
		"outputs": map[string]any{"amount": 10}})
	status, asked, raw := h.deviate(t, boss, instanceID, apply)
	if status != http.StatusAccepted || asked.PendingApproval == nil {
		t.Fatalf("asking: %d (%s)", status, raw)
	}
	requestID := asked.PendingApproval.RequestID
	before := h.everyRow(t)

	want := invalid(decisionCouldNotBeRead)
	bodies := map[string]string{
		"another field":               `{"verdict":"yes"}`,
		"the field in other letters":  `{"Reason":"x"}`,
		"the field said twice":        `{"reason":"a","reason":"b"}`,
		"a reason that is no text":    `{"reason":7}`,
		"something after it":          `{"reason":"x"} {}`,
		"a list":                      `[]`,
		"not JSON":                    `reason=x`,
		"null":                        `null`,
		"a command of its own":        `{"reason":"x","command":{"kind":"waive","node_id":"step","outputs":{"amount":250}}}`,
		"outputs of its own":          `{"reason":"x","outputs":{"amount":250}}`,
		"outputs and nothing else":    `{"outputs":{"amount":250}}`,
		"a visit key of its own":      `{"reason":"x","visit_key":"` + fmt.Sprint(apply["visit_key"]) + `"}`,
		"an instance of its own":      `{"reason":"x","instance_id":"` + instanceID.String() + `"}`,
		"a step of its own":           `{"reason":"x","node_id":"step"}`,
		"a kind of its own":           `{"reason":"x","kind":"cancel"}`,
		"dry_run":                     `{"reason":"x","dry_run":false}`,
		"who approved, said by them":  `{"reason":"x","decided_by":"boss"}`,
		"a status of its own":         `{"reason":"x","status":"applied"}`,
		"the request it is for":       `{"reason":"x","id":"` + requestID + `"}`,
		"the plan it would have":      `{"reason":"x","plan":{"requires_second_approver":false}}`,
		"an approver named in a body": `{"reason":"x","approved_by":"deputy"}`,
	}
	for _, path := range []string{requestPath(requestID) + "/approve", requestPath(requestID) + "/reject", requestPath("not-an-id") + "/approve"} {
		for name, body := range bodies {
			if status, raw := h.send(t, deputy, path, body); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
				t.Fatalf("%s with %s (%s): %d (%s), want 400 %s", path, name, body, status, raw, want)
			}
			for _, leak := range []string{"json:", "struct", "Go ", "DecideDeviationRequestRequest", "unmarshal"} {
				if strings.Contains(raw, leak) {
					t.Errorf("%s: the refusal leaks %q: %s", name, leak, raw)
				}
			}
		}
		h.requireUnchanged(t, before, "decisions that could not be read, sent to "+path)
	}
	// The body is read before anybody is asked what they may do, as on every
	// route. So a member who sends what cannot be read is told that, and one
	// who sends a decision is told they may not decide; neither is told
	// anything of the request. Nobody signed in is asked who they are first.
	approve := requestPath(requestID) + "/approve"
	if status, raw := h.send(t, member, approve, `{"outputs":{"amount":250}}`); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
		t.Errorf("a member sending a decision that cannot be read: %d (%s), want 400 %s", status, raw, want)
	}
	if status, raw := h.send(t, member, approve, `{"reason":"x"}`); status != http.StatusForbidden || !sameJSON(t, raw, refusal("forbidden", needsTheAdministratorRole)) {
		t.Errorf("a member sending a decision: %d (%s), want the gate's 403", status, raw)
	}
	if status, raw := h.send(t, "", approve, `{"outputs":`); status != http.StatusUnauthorized {
		t.Errorf("nobody sending what is not a decision: %d (%s), want 401", status, raw)
	}
	h.requireUnchanged(t, before, "decisions from somebody who may not decide")

	// A decision too large to be one is told so.
	tooLarge := invalid("this request is larger than 16 KiB, which is more than a decision needs to say")
	huge := `{"reason":"` + strings.Repeat("x", 17<<10) + `"}`
	if status, raw := h.send(t, deputy, requestPath(requestID)+"/approve", huge); status != http.StatusBadRequest || !sameJSON(t, raw, tooLarge) {
		t.Errorf("a decision of 17 KiB: %d (%.200s), want 400 %s", status, raw, tooLarge)
	}
	// The request is named in the address, and an address that names none is
	// the caller's to fix — on every route that takes one.
	notAnID := invalid(`request id "not-an-id" is not a valid identifier`)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, requestPath("not-an-id")},
		{http.MethodPost, requestPath("not-an-id") + "/approve"},
		{http.MethodPost, requestPath("not-an-id") + "/reject"},
	} {
		if status, raw, _ := h.call(t, route.method, deputy, route.path, ""); status != http.StatusBadRequest || !sameJSON(t, raw, notAnID) {
			t.Fatalf("%s %s: %d (%s), want 400 %s", route.method, route.path, status, raw, notAnID)
		}
	}
	h.requireUnchanged(t, before, "decisions that could not be read")
	if h.requestStatus(t, requestID) != "pending_approval" || !h.stepIsOpen(t, instanceID) {
		t.Fatal("decisions that could not be read changed the request or the instance")
	}

	// What is nearly nothing is still a decision: no body, an empty object,
	// spaces — an approval with no note. And what it approves is what was
	// asked for: amount 10, so "Book it", whatever the refused bodies said.
	if status, raw := h.send(t, deputy, requestPath(requestID)+"/approve", "  \n"); status != http.StatusOK {
		t.Fatalf("an approval with a body of spaces: %d (%s), want it approved", status, raw)
	}
	if h.openTasksOn(t, instanceID, "small") != 1 || h.openTasksOn(t, instanceID, "large") != 0 {
		t.Fatal("the approved waive did not go where the sealed command's amount sends it")
	}
	instance, err := h.svc.GetInstance(h.tenantContext(), instanceID)
	if err != nil || instance.Variables["amount"] != float64(10) {
		t.Fatalf("the instance holds amount = %v (err %v), want the 10 that was asked for", instance.Variables["amount"], err)
	}
}

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

// B3 over the API: whoever asked does not approve their own request, with a
// reason or without, and the refusal changes nothing. They may end it: a
// rejection by the requester is a withdrawal, and is not an approval by
// anybody.
func TestTheRequesterIsRefusedTheirOwnRequestOverTheAPIAndMayWithdrawIt(t *testing.T) {
	h := newDeviationRouteHarness(t)
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
	for _, account := range []string{"requested_by_id", "decided_by_id", h.accountID(t, "boss").String(), h.accountID(t, "deputy").String()} {
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

// The queue is what an administrator can act on: what still waits, newest
// first, a page at a time, with how many there are in all. A listed request
// is not a whole one. The queue does not read what was asked, what the
// requester was shown or which instances it covers, so it does not write
// them — the fields are absent, not null and not empty, which would read as
// "asked for nothing" — and the single read has them.
func TestARequestIsListedWithoutWhatOnlyTheSingleReadHas(t *testing.T) {
	h := newDeviationRouteHarness(t)
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
	accounts := []string{"requested_by_id", "decided_by_id", h.accountID(t, "boss").String(), h.accountID(t, "deputy").String()}

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

// behindNoGate serves the deviation routes with nothing in front of them: the
// real handlers, decoders and endpoints over the real service, and no chain
// that signs anybody in or checks a role. Every request to it is made as
// caller, acting in the harness's organization.
//
// It is how a test asks what the service itself answers a caller the gate
// would have turned away, or one the gate cannot produce — through the route,
// so that the status is the route's.
func (h *deviationHarness) behindNoGate(t *testing.T, caller entities.User) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	deviations.RegisterHandlers(mux, deviation.MakeEndpoints(h.svc), []httptransport.ServerOption{
		httptransport.ServerErrorEncoder(common.EncodeError),
		httptransport.ServerBefore(func(ctx context.Context, _ *http.Request) context.Context {
			return context.WithValue(entities.WithTenantContext(ctx, entities.TenantContext{TenantID: h.orgID.String()}), pkgauth.UserContextKey, caller)
		}),
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// ask sends one request to server and answers the status and the reply.
func ask(t *testing.T, server *httptest.Server, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := routeClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the reply: %v", err)
	}
	return resp.StatusCode, strings.TrimSpace(string(raw))
}

// AGENTS.md §2.3: a fast path that skips a check is a vulnerability. The
// routes are gated, and the service asks again who is calling: reached with
// no gate in front of it, each of the four refuses whoever is not an
// administrator of the organization, and an administrator whose sign-in
// carries no account id — the requester and the approver are told apart by
// account, so a caller with none cannot be shown to be somebody else. Each is
// a 403, in the service's words, and writes nothing.
func TestTheServiceRefusesWhoeverTheGateWouldHaveBehindEachRequestRoute(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.oneStep(t)
	boss := h.signIn(t, "boss", entities.RoleAdmin)
	h.signIn(t, "deputy", entities.RoleAdmin)
	requestID := h.askToWaive(t, boss, instanceID)
	decision := fmt.Sprintf(`{"reason":%q}`, decisionReason)
	routes := []struct{ name, method, path, body string }{
		{"the queue", http.MethodGet, requestsPath, ""},
		{"the read", http.MethodGet, requestPath(requestID), ""},
		{"the approval", http.MethodPost, requestPath(requestID) + "/approve", decision},
		{"the rejection", http.MethodPost, requestPath(requestID) + "/reject", decision},
	}
	notAnAdministrator := refusal("forbidden", "only an administrator can ask for or decide a request for a second administrator")
	noAccount := refusal("forbidden", "asking for and giving a second administrator's approval needs an account, and this request carries none")
	callers := []struct {
		name  string
		who   entities.User
		reply string
	}{
		{"a member", entities.User{ID: h.accountID(t, "deputy"), Username: "deputy", Roles: []string{entities.RoleUser}}, notAnAdministrator},
		{"an operator and designer", entities.User{ID: uuid.Must(uuid.NewV7()), Username: "ops", Roles: []string{entities.RoleOperator, entities.RoleDesigner}}, notAnAdministrator},
		{"an administrator with no name", entities.User{ID: uuid.Must(uuid.NewV7()), Roles: []string{entities.RoleAdmin}}, notAnAdministrator},
		{"an administrator of another organization only", entities.User{ID: uuid.Must(uuid.NewV7()), Username: "elsewhere",
			RolesByOrganization: map[uuid.UUID][]string{uuid.Must(uuid.NewV7()): {entities.RoleAdmin}}}, notAnAdministrator},
		{"an administrator with no account id", entities.User{Username: "deputy", Roles: []string{entities.RoleAdmin}}, noAccount},
	}
	before := h.everyRow(t)
	for _, caller := range callers {
		server := h.behindNoGate(t, caller.who)
		for _, route := range routes {
			status, raw := ask(t, server, route.method, route.path, route.body)
			if status != http.StatusForbidden || !sameJSON(t, raw, caller.reply) {
				t.Fatalf("%s on %s, with no gate in front: %d (%s), want 403 %s", caller.name, route.name, status, raw, caller.reply)
			}
			h.requireUnchanged(t, before, caller.name+" on "+route.name+", with no gate in front")
		}
	}
	if h.requestStatus(t, requestID) != "pending_approval" || !h.stepIsOpen(t, instanceID) {
		t.Fatal("refused calls changed the request or the instance")
	}
}
