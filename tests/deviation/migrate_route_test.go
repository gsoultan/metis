package deviation_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/tests/testutils"
)

// What the migrate route answers, read off the wire: its statuses, the shape
// of each reply, and — for an instance a run left alone — why, as a code a
// client can translate beside the sentence the reply has always carried.

const (
	migratePath = "/api/v1/definitions/versions/migrate"
	// skipsTheApproval is the one reason the tests' migration needs a second
	// administrator, as the plan, the waiting request and the request read
	// by itself all give it.
	skipsTheApproval = "“Operations approve” would be skipped for every listed instance waiting at it when the migration runs — " +
		"not only those waiting there when this was asked for — and nobody would perform it"
)

// migrateWith sends body to the migrate route and answers the status and the
// reply. It is migrate for a migration other than the one that skips the
// approval.
func (h *deviationHarness) migrateWith(t *testing.T, token string, body map[string]any) (int, migrateReply, string) {
	t.Helper()
	status, raw := h.do(t, http.MethodPost, token, migratePath, body)
	var reply migrateReply
	if status >= 200 && status < 300 {
		if err := json.Unmarshal([]byte(raw), &reply); err != nil {
			t.Fatalf("decode the migrate reply: %v (%s)", err, raw)
		}
	}
	return status, reply, strings.TrimSpace(raw)
}

// passedOverEntry is one instance a reply says a run left alone, as it is
// written: the instance, the cause as a code, the steps the cause is about
// and the sentence.
func passedOverEntry(instanceID uuid.UUID, cause, reason string, steps ...[2]string) string {
	named := make([]map[string]string, 0, len(steps))
	for _, step := range steps {
		named = append(named, map[string]string{"node_id": step[0], "name": step[1]})
	}
	written, err := json.Marshal([]map[string]any{{"instance_id": instanceID.String(), "cause": cause, "steps": named, "reason": reason}})
	if err != nil {
		panic(err)
	}
	return string(written)
}

// requirePassedOver fails unless the reply's passed_over is exactly want — an
// entry with the four fields a client reads and no other — and its cause is
// one the server's closed set names.
func requirePassedOver(t *testing.T, raw, want string) {
	t.Helper()
	written := object(t, raw)
	got, err := json.Marshal(written["passed_over"])
	if err != nil {
		t.Fatalf("encode passed_over: %v", err)
	}
	if !sameJSON(t, string(got), want) {
		t.Fatalf("passed_over is\n  %s\nwant\n  %s", got, want)
	}
	entries, _ := written["passed_over"].([]any)
	for _, entry := range entries {
		requireFields(t, entry, "an instance passed over", "instance_id", "cause", "steps", "reason")
		cause, _ := entry.(map[string]any)["cause"].(string)
		if !entities.PassedOverCause(cause).Valid() {
			t.Fatalf("the cause %q is none the server names", cause)
		}
	}
}

// planFieldsOfASkip is the fields the migration plan has, on either route,
// for the tests' migration: what it always had — refusals and warnings left
// out, as it has none — and the two that say it needs a second administrator.
var planFieldsOfASkip = []string{"source_key", "source_version", "target_version", "target_id", "instances", "moves", "compliance_holds",
	"actions", "removed_nodes", "requires_second_approver", "second_approver_reasons"}

// B8, and the route's statuses. A dry run is a 200 and asks nobody. An apply
// that skips a step is not made: it is a 202 that names the request now
// waiting, and so is the same apply sent again by whoever asked. A different
// administrator's is a 400 that points at that request. The approval that
// runs it is a 200. Nothing in any of them is an account id or null.
func TestAMigrationThatSkipsAStepIsSentForApproval(t *testing.T) {
	h := newDeviationRouteHarness(t)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	m := h.twoVersionsToMigrate(t)
	instanceID := h.waitingAtTheApproval(t)

	before := h.everyRow(t)
	status, preview, raw := h.migrate(t, boss, m, true)
	if status != http.StatusOK || preview.PendingApproval != nil || preview.Applied == nil || *preview.Applied ||
		preview.Plan["requires_second_approver"] != true || !strings.Contains(raw, `"passed_over":[]`) {
		t.Fatalf("the dry run: %d (%s), want 200 and the plan saying it needs a second administrator", status, raw)
	}
	requireFields(t, object(t, raw), "a dry run", "plan", "applied", "passed_over")
	requireFields(t, preview.Plan, "the plan of a dry run", planFieldsOfASkip...)
	if reasons, _ := preview.Plan["second_approver_reasons"].([]any); len(reasons) != 1 || reasons[0] != skipsTheApproval {
		t.Fatalf("why the dry run says it needs somebody else: %v", preview.Plan["second_approver_reasons"])
	}
	h.requireUnchanged(t, before, "a dry run")

	status, asked, raw := h.migrate(t, boss, m, false)
	if status != http.StatusAccepted {
		t.Fatalf("an apply that skips a step: %d (%s), want 202: it was taken, and nothing has been done", status, raw)
	}
	if asked.PendingApproval == nil || asked.Applied == nil || *asked.Applied || !strings.Contains(raw, `"passed_over":[]`) {
		t.Fatalf("the 202: %s, want nothing applied, nobody passed over and the request that waits", raw)
	}
	written := object(t, raw)
	requireFields(t, written, "an apply sent for approval", "plan", "applied", "passed_over", "pending_approval")
	requireFields(t, written["pending_approval"], "the request that waits", "request_id", "status", "requested_by", "expires_at", "because")
	requireFields(t, written["plan"], "the plan of an apply sent for approval", planFieldsOfASkip...)
	pending := asked.PendingApproval
	if pending.Status != "pending_approval" || pending.RequestedBy != "boss" || !slices.Equal(pending.Because, []string{skipsTheApproval}) ||
		time.Until(pending.ExpiresAt) < 71*time.Hour || time.Until(pending.ExpiresAt) > 73*time.Hour {
		t.Fatalf("the request that waits: %+v", pending)
	}
	if nulls := nullsIn(written, "reply"); len(nulls) != 0 {
		t.Fatalf("the 202 holds null at %v: %s", nulls, raw)
	}
	if strings.Contains(raw, h.accountID(t, "boss").String()) || strings.Contains(raw, "_by_id") {
		t.Fatalf("the 202 carries an account id: %s", raw)
	}
	if h.versionOf(t, instanceID) != m.v1 || h.requestCount(t) != 1 {
		t.Fatalf("asking moved the instance, or left %d requests", h.requestCount(t))
	}

	// Sent again by whoever asked: that answer again, and nothing written.
	waiting := h.everyRow(t)
	status, again, raw := h.migrate(t, boss, m, false)
	if status != http.StatusAccepted || again.Applied == nil || *again.Applied {
		t.Fatalf("the same apply sent again: %d (%s), want 202 and nothing applied", status, raw)
	}
	if !samePending(again, asked) {
		t.Fatalf("the same apply sent again names %+v, want %+v", again.PendingApproval, pending)
	}
	h.requireUnchanged(t, waiting, "the same apply sent again by whoever asked")

	// Another administrator applying the same migration is pointed at it.
	want := invalid(fmt.Sprintf("The same migration is already waiting for approval (request %s, asked by boss); approve or reject that one.", pending.RequestID))
	if status, _, raw := h.migrate(t, deputy, m, false); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
		t.Fatalf("the same migration applied by another administrator: %d (%s), want 400 %s", status, raw, want)
	}
	h.requireUnchanged(t, waiting, "another administrator's apply of a migration that waits")

	// What the second administrator reads (rulings §14): which instances the
	// request lists, and that the skip reaches each of them that waits at the
	// step when the migration runs — not only those waiting there now.
	status, shown, raw := h.readRequest(t, deputy, pending.RequestID)
	if status != http.StatusOK || !slices.Equal(shown.Request.Because, []string{skipsTheApproval}) ||
		!slices.Equal(shown.Request.Instances, []string{instanceID.String()}) {
		t.Fatalf("deputy reads the request: %d (%s), want the listed instance and what the skip reaches", status, raw)
	}

	// The approval runs it: a 200, with the plan as the migrate route writes it.
	status, approved, raw := h.decide(t, deputy, pending.RequestID, "approve", decisionReason)
	if status != http.StatusOK || !approved.Applied || approved.Request.Status != "applied" || !strings.Contains(raw, `"passed_over":[]`) {
		t.Fatalf("deputy approves: %d (%s), want 200 and the run applied", status, raw)
	}
	requireFields(t, object(t, raw)["plan"], "the plan of an approved migration", planFieldsOfASkip...)
	if !sameJSON(t, encoded(t, object(t, raw)["plan"]), encoded(t, written["plan"])) {
		t.Fatalf("the approval's plan is\n  %s\nand the plan the requester was answered with\n  %s", encoded(t, object(t, raw)["plan"]), encoded(t, written["plan"]))
	}
	if h.versionOf(t, instanceID) != m.v2 {
		t.Fatal("the approved migration did not move the instance")
	}
}

// samePending reports whether two replies name the same waiting request, in
// every field.
func samePending(a, b migrateReply) bool {
	if a.PendingApproval == nil || b.PendingApproval == nil {
		return false
	}
	x, y := *a.PendingApproval, *b.PendingApproval
	return x.RequestID == y.RequestID && x.Status == y.Status && x.RequestedBy == y.RequestedBy &&
		x.ExpiresAt.Equal(y.ExpiresAt) && slices.Equal(x.Because, y.Because)
}

// encoded is value as JSON.
func encoded(t *testing.T, value any) string {
	t.Helper()
	written, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return string(written)
}

// Design §8, the changed route: only an administrator of the organization
// migrates, and so only one asks. Everybody else is refused exactly as
// before the route could ask — and is answered the same whether or not a
// request waits, so that a refusal tells nobody that one does. No refusal
// writes anything.
func TestOnlyAnAdministratorOfTheOrganizationAsksForAMigrationApproval(t *testing.T) {
	h := newDeviationRouteHarness(t)
	boss := h.signIn(t, "boss", entities.RoleAdmin)
	m := h.twoVersionsToMigrate(t)
	instanceID := h.waitingAtTheApproval(t)
	const noRole = "this needs the ADMIN role, which your account does not hold in this organization; an administrator here can grant it"
	callers := []struct {
		name   string
		token  string
		status int
		want   string
	}{
		{"a member", h.signIn(t, "member", entities.RoleUser), http.StatusForbidden, refusal("forbidden", noRole)},
		{"an operator", h.signIn(t, "operator", entities.RoleOperator), http.StatusForbidden, refusal("forbidden", noRole)},
		{"a designer", h.signIn(t, "designer", entities.RoleDesigner), http.StatusForbidden, refusal("forbidden", noRole)},
		// The versions are not theirs to see: they are answered as for a
		// version that is not there.
		{"another organization's administrator", h.signInElsewhere(t, "outsider-boss", entities.RoleAdmin), http.StatusNotFound,
			`{"error":"source definition: not found: no such definition"}`},
		{"nobody", "", http.StatusUnauthorized, "Unauthorized"},
	}
	// refusals is what each caller is answered, for a dry run and an apply.
	refusals := func(when string) map[string]string {
		t.Helper()
		before := h.everyRow(t)
		answered := map[string]string{}
		for _, caller := range callers {
			for _, dryRun := range []bool{true, false} {
				status, _, raw := h.migrate(t, caller.token, m, dryRun)
				if raw = strings.TrimSpace(raw); status != caller.status || raw != caller.want {
					t.Fatalf("%s, %s (dry run %v): %d (%s), want %d %s", caller.name, when, dryRun, status, raw, caller.status, caller.want)
				}
				for _, told := range []string{"pending_approval", "request", "approval", skipReason, "Operations approve", "opsApprove"} {
					if strings.Contains(raw, told) {
						t.Fatalf("%s, %s, was told %q: %s", caller.name, when, told, raw)
					}
				}
				answered[fmt.Sprintf("%s (dry run %v)", caller.name, dryRun)] = raw
			}
		}
		h.requireUnchanged(t, before, "the refused migrations "+when)
		return answered
	}

	nothingWaits := refusals("with no request waiting")
	if n := h.requestCount(t); n != 0 {
		t.Fatalf("refused callers left %d request(s)", n)
	}
	requestID := h.askToMigrate(t, boss, m)
	aRequestWaits := refusals("while a request waits")
	for caller, raw := range aRequestWaits {
		if raw != nothingWaits[caller] {
			t.Errorf("%s is answered\n  %s\nwhile a request waits and\n  %s\nwhen none does: the refusal says whether one waits", caller, raw, nothingWaits[caller])
		}
		if strings.Contains(raw, requestID) {
			t.Errorf("%s was told the request's id: %s", caller, raw)
		}
	}
	if h.requestCount(t) != 1 || h.requestStatus(t, requestID) != "pending_approval" || h.versionOf(t, instanceID) != m.v1 {
		t.Fatal("the refused callers changed the request, made another, or moved the instance")
	}
}

// B3 and rulings §6 for a migration, over the API: its requester cannot
// approve it, and nobody can once it has expired or been decided. Each
// refusal leaves the instance on the version it runs, and writes nothing —
// but the one that finds the request past its deadline, which records that.
func TestAMigrationRequestIsRefusedToItsRequesterAndOnceExpiredOrDecided(t *testing.T) {
	h := newDeviationRouteHarness(t)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	m := h.twoVersionsToMigrate(t)
	instanceID := h.waitingAtTheApproval(t)
	ask := func() string {
		t.Helper()
		status, asked, raw := h.migrate(t, boss, m, false)
		if status != http.StatusAccepted || asked.PendingApproval == nil {
			t.Fatalf("ask: %d (%s), want 202 and the request that waits", status, raw)
		}
		return asked.PendingApproval.RequestID
	}

	requestID := ask()
	before := h.everyRow(t)
	want := refusal("forbidden", anotherMustApprove)
	if status, _, raw := h.decide(t, boss, requestID, "approve", decisionReason); status != http.StatusForbidden || !sameJSON(t, raw, want) {
		t.Fatalf("boss approving his own migration: %d (%s), want 403 %s", status, raw, want)
	}
	h.requireUnchanged(t, before, "the requester's approval of their own migration")

	// Past its deadline: the first decision that finds it so records the
	// expiry, on the request and nowhere else, and is refused; the next finds
	// it expired and writes nothing.
	h.letTheDeadlinePass(t, requestID)
	before = h.everyRow(t)
	status, _, raw := h.decide(t, deputy, requestID, "approve", decisionReason)
	if status != http.StatusBadRequest || !strings.Contains(raw, "This request expired on ") ||
		!strings.Contains(raw, " before anybody approved it, so nothing was applied. Ask again if it is still needed.") {
		t.Fatalf("deputy's approval of an expired migration: %d (%s), want 400 saying it expired", status, raw)
	}
	if changed := testutils.TablesThatDiffer(before, h.everyRow(t)); len(changed) != 1 || !strings.HasPrefix(changed[0], "deviation_requests ") {
		t.Fatalf("the approval that found the request expired changed %v, want the request alone", changed)
	}
	expired := h.everyRow(t)
	for _, verb := range []string{"approve", "reject"} {
		status, _, raw := h.decide(t, deputy, requestID, verb, decisionReason)
		if status != http.StatusBadRequest || !strings.Contains(raw, "This request expired on ") {
			t.Fatalf("deputy's %s of an expired migration: %d (%s), want 400 saying it expired", verb, status, raw)
		}
	}
	h.requireUnchanged(t, expired, "deciding an expired migration request")
	if h.requestStatus(t, requestID) != "expired" || h.versionOf(t, instanceID) != m.v1 {
		t.Fatal("an expired migration request was not recorded as expired, or moved the instance")
	}

	// Decided: asked afresh and rejected, it is nobody's to decide again.
	fresh := ask()
	if fresh == requestID {
		t.Fatal("asking again after an expiry answered the expired request")
	}
	if status, _, raw := h.decide(t, deputy, fresh, "reject", "not this quarter"); status != http.StatusOK {
		t.Fatalf("deputy rejects: %d (%s)", status, raw)
	}
	before = h.everyRow(t)
	for _, verb := range []string{"approve", "reject"} {
		status, _, raw := h.decide(t, deputy, fresh, verb, decisionReason)
		if status != http.StatusBadRequest || !strings.Contains(raw, "deputy rejected this on ") {
			t.Fatalf("deputy's %s of a rejected migration: %d (%s), want 400 saying who rejected it", verb, status, raw)
		}
	}
	h.requireUnchanged(t, before, "deciding a rejected migration request")
	if h.requestStatus(t, fresh) != "rejected" || h.versionOf(t, instanceID) != m.v1 {
		t.Fatal("a decided migration request changed, or moved the instance")
	}
}

// Pin: a migration that needs nobody else still applies in one call, a 200
// with the fields it always had and no request anywhere.
func TestAMappingOnlyMigrationStillAppliesInOneCall(t *testing.T) {
	h := newDeviationRouteHarness(t)
	boss := h.signIn(t, "boss", entities.RoleAdmin)
	m := h.twoVersionsToMigrate(t)
	instanceID := h.waitingAtTheApproval(t)
	body := map[string]any{"source_definition_id": m.v1.String(), "target_definition_id": m.v2.String(),
		"node_mapping": map[string]string{"opsApprove": "sign"}}

	for _, dryRun := range []bool{true, false} {
		body["dry_run"] = dryRun
		status, reply, raw := h.migrateWith(t, boss, body)
		if status != http.StatusOK || reply.Applied == nil || *reply.Applied == dryRun || reply.PendingApproval != nil ||
			reply.Plan["requires_second_approver"] != false || !strings.Contains(raw, `"passed_over":[]`) {
			t.Fatalf("a mapping-only migration (dry run %v): %d (%s), want 200, nobody asked and nobody passed over", dryRun, status, raw)
		}
		requireFields(t, object(t, raw), "the reply to a migration that needs nobody else", "plan", "applied", "passed_over")
		requireFields(t, reply.Plan, "the plan of a migration that needs nobody else",
			"source_key", "source_version", "target_version", "target_id", "instances", "moves", "warnings", "compliance_holds", "removed_nodes",
			"requires_second_approver")
	}
	if h.versionOf(t, instanceID) != m.v2 || h.requestCount(t) != 0 {
		t.Fatalf("the mapping-only migration did not move the instance, or left %d request(s)", h.requestCount(t))
	}
}

// Pin: what the route refused before it could ask, it refuses as it did. A
// plan that refuses is a 200 for a dry run, which says why, and a 400 for an
// apply, which says the same — and a plan that refuses asks nobody, though it
// skips a step: a refusal is not sent for approval. Nothing is written.
func TestAMigrationThePlanRefusesIsRefusedAsBeforeAndAsksNobody(t *testing.T) {
	h := newDeviationRouteHarness(t)
	boss := h.signIn(t, "boss", entities.RoleAdmin)
	m := h.twoVersionsToMigrate(t)
	h.waitingAtTheApproval(t)
	versions := func(dryRun bool) map[string]any {
		return map[string]any{"source_definition_id": m.v1.String(), "target_definition_id": m.v2.String(), "dry_run": dryRun}
	}
	skipWithNoReason := func(dryRun bool) map[string]any {
		body := versions(dryRun)
		body["node_actions"] = map[string]any{"opsApprove": map[string]any{"kind": "skip"}}
		return body
	}

	before := h.everyRow(t)
	for name, body := range map[string]func(bool) map[string]any{
		"work with nowhere to land":   versions,
		"a skip that gives no reason": skipWithNoReason,
	} {
		status, preview, raw := h.migrateWith(t, boss, body(true))
		refusals, _ := preview.Plan["refusals"].([]any)
		if status != http.StatusOK || len(refusals) == 0 || preview.Applied == nil || *preview.Applied || preview.PendingApproval != nil {
			t.Fatalf("the dry run of %s: %d (%s), want 200 and a plan that says why it refuses", name, status, raw)
		}
		said := make([]string, 0, len(refusals))
		for _, refusal := range refusals {
			said = append(said, fmt.Sprint(refusal))
		}
		want := invalid(strings.Join(said, "; "))
		t.Logf("refused, and told: %s", want)
		if status, _, raw := h.migrateWith(t, boss, body(false)); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
			t.Fatalf("the apply of %s: %d (%s), want 400 %s", name, status, raw, want)
		}
	}
	// A version that is no id is refused before anything is read.
	malformed := versions(false)
	malformed["source_definition_id"] = "version-one"
	if status, _, raw := h.migrateWith(t, boss, malformed); status != http.StatusBadRequest || !strings.Contains(raw, `source_definition_id \"version-one\" is not a valid identifier`) {
		t.Fatalf("a version that is no id: %d (%s), want 400", status, raw)
	}
	h.requireUnchanged(t, before, "the refused migrations")
	if n := h.requestCount(t); n != 0 {
		t.Fatalf("refused migrations left %d request(s)", n)
	}
}

// Found while the gate was built: a version no instance is on is answered
// before anybody is asked. An apply that skips a step there is a 200 that
// says it was applied, passed nobody over and waits on nobody — there was
// nothing to skip, and nothing is written.
func TestAnApplyOverAVersionNobodyIsOnAsksNobodyAndAnswersEmpty(t *testing.T) {
	h := newDeviationRouteHarness(t)
	boss := h.signIn(t, "boss", entities.RoleAdmin)
	m := h.twoVersionsToMigrate(t)

	before := h.everyRow(t)
	for _, dryRun := range []bool{true, false} {
		status, reply, raw := h.migrate(t, boss, m, dryRun)
		if status != http.StatusOK || reply.Applied == nil || *reply.Applied == dryRun || reply.PendingApproval != nil ||
			reply.Plan["requires_second_approver"] != false || fmt.Sprint(reply.Plan["instances"]) != "0" || !strings.Contains(raw, `"passed_over":[]`) {
			t.Fatalf("a skip over a version nobody is on (dry run %v): %d (%s), want 200 with nobody asked", dryRun, status, raw)
		}
		if _, said := reply.Plan["second_approver_reasons"]; said {
			t.Fatalf("a plan that needs nobody else gives reasons for needing somebody: %s", raw)
		}
	}
	h.requireUnchanged(t, before, "a migration over a version nobody is on")
}

// twoApprovalsKey is the process of twoApprovals.
const twoApprovalsKey = "route-two-approvals"

// twoApprovals is start → first approval → second approval → sign → end,
// or — without its approvals — start → sign → end: two removed steps in a
// row, so that skipping the first leaves an instance on the second, which
// the new version does not have either.
func twoApprovals(projectID uuid.UUID, withApprovals bool) *entities.ProcessDefinition {
	nodes := []*entities.Node{
		{ID: "start", Type: entities.StartEvent, Outgoing: []string{"a1"}},
		{ID: "sign", Name: "Sign", Type: entities.UserTask, Assignee: "sasha", Incoming: []string{"a1"}, Outgoing: []string{"a4"}},
		{ID: "end", Type: entities.EndEvent, Incoming: []string{"a4"}},
	}
	flows := []*entities.SequenceFlow{{ID: "a4", SourceRef: "sign", TargetRef: "end"}}
	if !withApprovals {
		flows = append(flows, &entities.SequenceFlow{ID: "a1", SourceRef: "start", TargetRef: "sign"})
	} else {
		nodes[1].Incoming = []string{"a3"}
		nodes = append(nodes,
			&entities.Node{ID: "firstApprove", Name: "First approval", Type: entities.UserTask, Assignee: "fay", Incoming: []string{"a1"}, Outgoing: []string{"a2"}},
			&entities.Node{ID: "secondApprove", Name: "Second approval", Type: entities.UserTask, Assignee: "sol", Incoming: []string{"a2"}, Outgoing: []string{"a3"}})
		flows = append(flows,
			&entities.SequenceFlow{ID: "a1", SourceRef: "start", TargetRef: "firstApprove"},
			&entities.SequenceFlow{ID: "a2", SourceRef: "firstApprove", TargetRef: "secondApprove"},
			&entities.SequenceFlow{ID: "a3", SourceRef: "secondApprove", TargetRef: "sign"})
	}
	return &entities.ProcessDefinition{Project: &entities.Project{ID: projectID}, Key: twoApprovalsKey, Name: "Two approvals", Nodes: nodes, Flows: flows}
}

// deployAndStage deploys a process and stages the version that follows it,
// so that an instance started afterwards still starts on the first.
func (h *deviationHarness) deployAndStage(t *testing.T, first, second *entities.ProcessDefinition) migration {
	t.Helper()
	v1, err := h.svc.CreateDefinition(h.tenantContext(), first)
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	v2, err := h.svc.DeployDefinition(h.tenantContext(), second, false)
	if err != nil {
		t.Fatalf("stage v2: %v", err)
	}
	return migration{v1: v1, v2: v2}
}

// started starts an instance of the process key in the harness's project.
func (h *deviationHarness) started(t *testing.T, key string, variables map[string]any) uuid.UUID {
	t.Helper()
	id, err := h.svc.StartProcess(h.tenantContext(), h.projID, key, variables)
	if err != nil {
		t.Fatalf("start %s: %v", key, err)
	}
	return id
}

// skipOf is the body of an apply of m that skips the step given.
func skipOf(m migration, nodeID, reason string) map[string]any {
	return map[string]any{"source_definition_id": m.v1.String(), "target_definition_id": m.v2.String(), "dry_run": false,
		"node_actions": map[string]any{nodeID: map[string]any{"kind": "skip", "reason": reason}}}
}

// Rulings addendum §12, on the approve route. Nothing races: the skip that
// was approved is what moves the instance, onto the step after the one
// skipped, and the new version has no such step either. The skip stands and
// the instance is left on its version — and the approval's reply says which
// instance, why as a code, at which step by id and by name, and in the
// sentence it always carried. The request reads applied, and says one was
// acted on and one passed over: the same one.
func TestAnApprovedRunSaysOverTheAPIWhichInstanceItPassedOverAndWhy(t *testing.T) {
	h := newDeviationRouteHarness(t)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	m := h.deployAndStage(t, twoApprovals(h.projID, true), twoApprovals(h.projID, false))
	instanceID := h.started(t, twoApprovalsKey, nil)

	status, asked, raw := h.migrateWith(t, boss, skipOf(m, "firstApprove", skipReason))
	if status != http.StatusAccepted || asked.PendingApproval == nil {
		t.Fatalf("the apply that skips the first approval: %d (%s), want 202", status, raw)
	}
	status, approved, raw := h.decide(t, deputy, asked.PendingApproval.RequestID, "approve", decisionReason)
	if status != http.StatusOK || !approved.Applied || approved.Request.Status != "applied" {
		t.Fatalf("deputy approves: %d (%s), want 200: the skip was made, so something was applied", status, raw)
	}
	requireFields(t, object(t, raw), "the approval of a migration", "request", "applied", "plan", "passed_over")
	requirePassedOver(t, raw, passedOverEntry(instanceID, "nowhere_to_land",
		`When the migration came to move it, it had work at "Second approval", and version 2 has nowhere to put that, so it was not moved. `+
			`It stays on version 1. Plan the migration again for where it now stands: it needs a mapping, or a decision, for that work.`,
		[2]string{"secondApprove", "Second approval"}))
	if nulls := nullsIn(object(t, raw), "reply"); len(nulls) != 0 {
		t.Fatalf("the approval holds null at %v: %s", nulls, raw)
	}
	if outcome := approved.Request.Outcome; fmt.Sprint(outcome["changed"]) != "1" || fmt.Sprint(outcome["passed_over"]) != "1" || len(outcome) != 2 {
		t.Fatalf("the request's outcome %v, want one acted on and one passed over", outcome)
	}
	if h.versionOf(t, instanceID) != m.v1 || h.openTasksOn(t, instanceID, "secondApprove") != 1 {
		t.Fatal("the instance passed over is not waiting at the second approval on the version it ran")
	}
}

// Rulings addendum §12, on the migrate route's own reply: an apply that
// needed nobody else, and left an instance alone.
//
// The instance leaves its step between the apply listing it and locking it:
// its holder completes the step. That is two people at once, and the order
// is made here by a third holding the instance's row — the holder's
// completion waits for the row first, the migration's cancel behind it, and
// the row is then let go. Nothing is written by hand: the completion and the
// migration are the product's own, in the order the row gives them.
func TestTheMigrateRouteSaysWhichInstanceItPassedOverAndWhy(t *testing.T) {
	h := newDeviationRouteHarness(t)
	boss := h.signIn(t, "boss", entities.RoleAdmin)
	m := h.twoVersionsToMigrate(t)
	instanceID := h.waitingAtTheApproval(t)
	taskID := h.openTaskOf(t, instanceID)

	row := h.holdOpen(t, `SELECT 1 FROM process_instances WHERE id = ? FOR UPDATE`, instanceID)
	completed := make(chan error, 1)
	go func() { completed <- h.svc.CompleteTask(h.tenantContext(), taskID, "ollie", nil) }()
	row.untilBehind(completed)
	completion := h.sessionBehind(t, row.session)

	var got posted
	migrated := make(chan error, 1)
	go func() {
		var err error
		got, err = h.post(boss, migratePath, fmt.Sprintf(
			`{"source_definition_id":%q,"target_definition_id":%q,"dry_run":false,"node_actions":{"opsApprove":{"kind":"cancel","reason":"the quotation was withdrawn"}}}`,
			m.v1, m.v2))
		migrated <- err
	}()
	h.untilSomebodyIsBehind(t, completion, migrated)
	row.undo()
	if err := answerOf(t, completed, "the holder's completion"); err != nil {
		t.Fatalf("the holder's completion: %v", err)
	}
	if err := answerOf(t, migrated, "the migration"); err != nil {
		t.Fatalf("the migration: %v", err)
	}

	if got.status != http.StatusOK {
		t.Fatalf("the apply: %d (%s), want 200", got.status, got.raw)
	}
	written := object(t, got.raw)
	requireFields(t, written, "an apply that passed an instance over", "plan", "applied", "passed_over")
	if written["applied"] != false {
		t.Fatalf("applied is %v although nothing was written to any instance: %s", written["applied"], got.raw)
	}
	requirePassedOver(t, got.raw, passedOverEntry(instanceID, "left_the_step",
		`It was no longer waiting at "Operations approve" when the migration reached it, so nothing was decided there and it was not moved. `+
			`It stays on version 1; if it is still running, run the same migration again to plan for where it now stands.`,
		[2]string{"opsApprove", "Operations approve"}))
	if nulls := nullsIn(written, "reply"); len(nulls) != 0 {
		t.Fatalf("the reply holds null at %v: %s", nulls, got.raw)
	}
	if h.versionOf(t, instanceID) != m.v1 || h.openTasksOn(t, instanceID, "sign") != 1 || h.rowCount(t, instanceID) != 0 {
		t.Fatal("the instance passed over is not waiting at the next step on the version it ran, with nothing in its ledger")
	}
}

// sessionBehind is the one session waiting for a lock the session given holds.
func (h *deviationHarness) sessionBehind(t *testing.T, session int) int {
	t.Helper()
	var waiting []int
	if err := h.db.Raw(`SELECT a.pid FROM pg_stat_activity a WHERE ? = ANY(pg_blocking_pids(a.pid))`, session).Scan(&waiting).Error; err != nil {
		t.Fatalf("look for the session waiting: %v", err)
	}
	if len(waiting) != 1 {
		t.Fatalf("%d sessions wait behind the one that holds the row, want the one", len(waiting))
	}
	return waiting[0]
}

// untilSomebodyIsBehind waits until a session waits for a lock the session
// given holds or is in line for. It fails the test if what was sent finishes
// first: then it never waited.
func (h *deviationHarness) untilSomebodyIsBehind(t *testing.T, session int, finished <-chan error) {
	t.Helper()
	for deadline := time.Now().Add(raceWait); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		select {
		case err := <-finished:
			t.Fatalf("what was sent finished (%v) without waiting", err)
		default:
		}
		var waiting int
		if err := h.db.Raw(`SELECT count(*) FROM pg_stat_activity a WHERE ? = ANY(pg_blocking_pids(a.pid))`, session).Scan(&waiting).Error; err != nil {
			t.Fatalf("look for the session waiting: %v", err)
		}
		if waiting > 0 {
			return
		}
	}
	t.Fatalf("nothing was waiting behind session %d after %s", session, raceWait)
}

// claimKey is the process of claim.
const claimKey = "route-claim"

// claim is start → review → a gateway that takes "approved" to one end and
// "rejected" to the other, and has no way out for a claim that is neither;
// or, without the review, start → the gateway. Skipping the review of a
// claim nobody has decided leaves the gateway nothing to follow.
func claim(projectID uuid.UUID, withReview bool) *entities.ProcessDefinition {
	into := "in"
	if withReview {
		into = "out"
	}
	nodes := []*entities.Node{
		{ID: "start", Type: entities.StartEvent, Outgoing: []string{"in"}},
		{ID: "decide", Type: entities.ExclusiveGateway, Incoming: []string{into}, Outgoing: []string{"yes", "no"}},
		{ID: "accepted", Type: entities.EndEvent, Incoming: []string{"yes"}},
		{ID: "rejected", Type: entities.EndEvent, Incoming: []string{"no"}},
	}
	flows := []*entities.SequenceFlow{
		{ID: "yes", SourceRef: "decide", TargetRef: "accepted", Condition: "approved"},
		{ID: "no", SourceRef: "decide", TargetRef: "rejected", Condition: "rejected"},
	}
	if withReview {
		nodes = append(nodes, &entities.Node{ID: "review", Name: "Review the claim", Type: entities.UserTask, Assignee: "rita",
			Properties: testutils.FormDeclaring("approved"), Incoming: []string{"in"}, Outgoing: []string{"out"}})
		flows = append(flows,
			&entities.SequenceFlow{ID: "in", SourceRef: "start", TargetRef: "review"},
			&entities.SequenceFlow{ID: "out", SourceRef: "review", TargetRef: "decide"})
	} else {
		flows = append(flows, &entities.SequenceFlow{ID: "in", SourceRef: "start", TargetRef: "decide"})
	}
	return &entities.ProcessDefinition{Project: &entities.Project{ID: projectID}, Key: claimKey, Name: "Claim", Nodes: nodes, Flows: flows}
}

// An approved run that stops part-way is answered as the failure it is — not
// as a 200 that carries what the run did. How far it got is on the request,
// which reads interrupted: how many instances it had acted on, how many it
// had passed over, and that it stopped. A client that was answered the
// failure reads that from the request itself.
//
// Two claims wait for their review. One was started already approved, so
// the gateway after its skipped review has a way out; the other was not, and
// has none. A run takes the newest instance first: the approved claim is
// started last, so the run acts on it and then stops at the other.
func TestARunThatStoppedPartWayIsAnsweredAsAFailureAndTheRequestSaysHowFarItGot(t *testing.T) {
	h := newDeviationRouteHarness(t)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	m := h.deployAndStage(t, claim(h.projID, true), claim(h.projID, false))
	undecided := h.started(t, claimKey, nil)
	decided := h.started(t, claimKey, map[string]any{"approved": true})

	status, asked, raw := h.migrateWith(t, boss, skipOf(m, "review", "claims are no longer reviewed"))
	if status != http.StatusAccepted || asked.PendingApproval == nil {
		t.Fatalf("the apply that skips the review: %d (%s), want 202", status, raw)
	}
	requestID := asked.PendingApproval.RequestID

	status, _, raw = h.decide(t, deputy, requestID, "approve", decisionReason)
	if status != http.StatusInternalServerError {
		t.Fatalf("the approval whose run stopped part-way: %d (%s), want the failure's own status", status, raw)
	}
	requireFields(t, object(t, raw), "the reply to an approval whose run stopped", "error")
	said, _ := object(t, raw)["error"].(string)
	if !strings.HasPrefix(said, `the approved migration did not finish: advancing instance `+undecided.String()+` past "review": `) ||
		!strings.Contains(said, " (1 of 2 instances had already been dealt with). ") ||
		!strings.HasSuffix(said, "Request "+requestID+
			" now reads interrupted; what its run had done stands, and what remains has to be asked for again") {
		t.Fatalf("the failure says %q, want which instance it stopped at, how many it had dealt with, and what became of the request", said)
	}
	// One thing to do, and the true one: the request is spent, so the run's
	// own "run the same migration again" is not said. Nor is the engine's
	// marker for an error a process may catch, which is no word.
	if strings.Contains(said, "run the same migration again") || strings.Contains(said, "BPMN_ERROR") {
		t.Fatalf("the failure tells the approver to run the migration again, or carries the engine's marker: %q", said)
	}

	// How far it got, from the request.
	status, read, raw := h.readRequest(t, boss, requestID)
	if status != http.StatusOK || read.Request.Status != "interrupted" || read.Request.DecidedBy != "deputy" || read.Request.DecisionReason != decisionReason {
		t.Fatalf("the request after the failed run: %d (%s), want it interrupted, approved by deputy", status, raw)
	}
	outcome := read.Request.Outcome
	if fmt.Sprint(outcome["changed"]) != "1" || fmt.Sprint(outcome["passed_over"]) != "0" || len(outcome) != 3 ||
		outcome["error"] != "the run stopped on a failure after it had acted on 1 instance(s); what it had done by then stands" {
		t.Fatalf("the request's outcome %v, want how many the run had acted on and passed over, and that it stopped", outcome)
	}
	// What it had done stands, and what it had not is as it was.
	if h.rowCount(t, decided) != 1 || h.openTasksOn(t, decided, "review") != 0 {
		t.Fatal("the claim the run acted on has no record of its skip, or still waits for its review")
	}
	if h.rowCount(t, undecided) != 0 || h.openTasksOn(t, undecided, "review") != 1 || h.versionOf(t, undecided) != m.v1 {
		t.Fatal("the claim the run stopped at was changed")
	}
	// Nobody decides it a second time, and what remains can be asked for.
	if status, _, raw := h.decide(t, deputy, requestID, "approve", ""); status != http.StatusBadRequest || !strings.Contains(raw, "the run stopped part-way") {
		t.Fatalf("approving an interrupted request again: %d (%s), want 400", status, raw)
	}
	status, again, raw := h.migrateWith(t, boss, skipOf(m, "review", "claims are no longer reviewed"))
	if status != http.StatusAccepted || again.PendingApproval == nil || again.PendingApproval.RequestID == requestID {
		t.Fatalf("asking again for what remains: %d (%s), want 202 and a new request", status, raw)
	}
}
