package deviation_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// deviateObject sends a request the route must answer 200 and gives the reply
// as it is written, field by field.
func (h *deviationHarness) deviateObject(t *testing.T, token string, instanceID uuid.UUID, body map[string]any) (map[string]any, string) {
	t.Helper()
	status, _, raw := h.deviate(t, token, instanceID, body)
	if status != http.StatusOK {
		t.Fatalf("%v: %d (%s), want 200", body, status, raw)
	}
	var reply map[string]any
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	return reply, raw
}

// fieldsOf is the names an object of a reply has, in order.
func fieldsOf(t *testing.T, value any, what string) []string {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s is %T, want an object", what, value)
	}
	names := make([]string, 0, len(object))
	for name := range object {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func requireFields(t *testing.T, value any, what string, want ...string) {
	t.Helper()
	slices.Sort(want)
	if got := fieldsOf(t, value, what); !slices.Equal(got, want) {
		t.Fatalf("%s has the fields %v, want %v", what, got, want)
	}
}

// nullsIn names every place under value that holds null.
func nullsIn(value any, at string) []string {
	var found []string
	switch v := value.(type) {
	case nil:
		found = append(found, at)
	case map[string]any:
		for name, inner := range v {
			found = append(found, nullsIn(inner, at+"."+name)...)
		}
	case []any:
		for i, inner := range v {
			found = append(found, nullsIn(inner, fmt.Sprintf("%s[%d]", at, i))...)
		}
	}
	slices.Sort(found)
	return found
}

// notSnakeCase names every field of the given objects that is not written in
// lower case with underscores.
func notSnakeCase(t *testing.T, objects map[string]any) []string {
	t.Helper()
	var found []string
	for what, value := range objects {
		for _, name := range fieldsOf(t, value, what) {
			if strings.IndexFunc(name, func(r rune) bool { return !unicode.IsLower(r) && r != '_' }) >= 0 {
				found = append(found, what+"."+name)
			}
		}
	}
	slices.Sort(found)
	return found
}

var (
	planFields = []string{"instance_id", "kind", "scope", "node_id", "node_name", "visit_key", "open_work", "open_work_in_all",
		"outputs", "decision_points", "decision_points_in_all", "missing", "missing_in_all", "called_instances", "called_instances_in_all",
		"requires_second_approver", "refusals", "warnings", "applicable"}
	pointFields = []string{"node_id", "node_name", "kind", "reads", "reads_in_all", "supplied", "missing", "missing_in_all",
		"has_default_flow", "analysed"}
)

// What the route answers is read by scripts and by a screen. It has one shape
// whatever it holds: every list is a list, empty rather than null; every name
// is written as the neighbouring routes write theirs; a plan says what is
// missing in full and how many of everything there are beside what it lists;
// and nobody's account id is in it.
func TestTheReplyToADeviationHasOneShapeWhateverItHolds(t *testing.T) {
	h := newDeviationRouteHarness(t)
	// The step's holder is an account, as the administrator is: both have an
	// id that a reply could carry.
	h.signIn(t, "alice", entities.RoleUser)
	instanceID := h.start(t, orderBySize())
	admin := h.signIn(t, "boss", entities.RoleAdmin)
	before := h.everyRow(t)
	var replies []string

	// A plan that refuses: the gateway after the step reads amount, and the
	// request says nothing of it.
	preview := map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason}
	reply, raw := h.deviateObject(t, admin, instanceID, preview)
	replies = append(replies, raw)
	requireFields(t, reply, "a preview", "plan", "applied", "replayed")
	plan := reply["plan"].(map[string]any)
	requireFields(t, plan, "the plan", planFields...)
	for name, want := range map[string]any{
		"instance_id": instanceID.String(), "kind": "waive", "scope": "task", "node_id": "step", "node_name": "Check the order",
		"open_work_in_all": float64(1), "decision_points_in_all": float64(1), "missing_in_all": float64(1),
		"requires_second_approver": true, "applicable": false,
	} {
		if plan[name] != want {
			t.Errorf("the plan's %s is %v, want %v", name, plan[name], want)
		}
	}
	for name, want := range map[string]string{
		"missing": `["amount"]`, "outputs": `{}`, "called_instances": `[]`,
		"refusals": `["“Large order?” decides from amount, which “Check the order” would have set; say what the waiver counts as by supplying amount."]`,
	} {
		if got, _ := json.Marshal(plan[name]); string(got) != want {
			t.Errorf("the plan's %s is %s, want %s", name, got, want)
		}
	}
	work := plan["open_work"].([]any)
	if len(work) != 1 {
		t.Fatalf("the plan lists %d open tasks, want the one: %s", len(work), raw)
	}
	requireFields(t, work[0], "an open task", "task_id", "name", "node_id", "node_name", "status", "assignee")
	points := plan["decision_points"].([]any)
	if len(points) != 1 {
		t.Fatalf("the plan lists %d decision points, want the gateway: %s", len(points), raw)
	}
	requireFields(t, points[0], "a decision point", pointFields...)
	if got, _ := json.Marshal(points[0]); !sameJSON(t, string(got), `{"node_id":"size","node_name":"Large order?","kind":"gateway",
		"reads":["amount"],"reads_in_all":1,"supplied":[],"missing":["amount"],"missing_in_all":1,"has_default_flow":false,"analysed":true}`) {
		t.Errorf("the gateway reads as %s", got)
	}
	if nulls := nullsIn(reply, "reply"); len(nulls) != 0 {
		t.Errorf("the preview holds null at %v: %s", nulls, raw)
	}
	if odd := notSnakeCase(t, map[string]any{"reply": reply, "plan": plan, "open_work": work[0], "decision_point": points[0]}); len(odd) != 0 {
		t.Errorf("fields not written as the other routes write theirs: %v", odd)
	}

	// The same, saying what the waiver counts as: nothing is missing.
	preview["outputs"] = map[string]any{"amount": 250}
	apply, _ := h.previewed(t, admin, instanceID, preview)
	reply, raw = h.deviateObject(t, admin, instanceID, preview)
	replies = append(replies, raw)
	plan = reply["plan"].(map[string]any)
	if work := plan["open_work"].([]any); len(work) != 1 || work[0].(map[string]any)["assignee"] != "alice" {
		t.Fatalf("the plan does not name the step's holder: %s", raw)
	}
	point, _ := json.Marshal(plan["decision_points"].([]any)[0])
	if plan["applicable"] != true || plan["missing_in_all"] != float64(0) || !strings.Contains(raw, `"missing":[]`) ||
		!strings.Contains(raw, `"refusals":[]`) || !strings.Contains(raw, `"outputs":{"amount":250}`) ||
		!strings.Contains(string(point), `"supplied":["amount"]`) || !strings.Contains(string(point), `"missing":[]`) {
		t.Fatalf("a preview that supplies the amount: %s", raw)
	}
	h.requireUnchanged(t, before, "previews")
	h.secondAdministrator(t)

	// Asked: a waive waits for a second administrator, and the reply is a
	// 202 that says so — the plan it was asked with, the record as it waits,
	// and the request it waits on.
	status, asked, raw := h.deviate(t, admin, instanceID, apply)
	if status != http.StatusAccepted || asked.Applied || asked.Replayed || asked.PendingApproval == nil {
		t.Fatalf("the apply of a waive: %d (%s), want a 202 that names the request it waits on", status, raw)
	}
	replies = append(replies, raw)
	reply = map[string]any{}
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	requireFields(t, reply, "a request", "plan", "applied", "replayed", "deviation", "pending_approval")
	requireFields(t, reply["plan"], "the plan asked with", planFields...)
	requireFields(t, reply["pending_approval"], "what it waits on", "request_id", "status", "requested_by", "expires_at", "because")
	waiting := reply["deviation"].(map[string]any)
	if waiting["status"] != "pending_approval" || waiting["actor"] != "boss" || waiting["request_id"] != asked.PendingApproval.RequestID {
		t.Fatalf("the record of a waive that waits: %s", raw)
	}
	if nulls := nullsIn(reply, "reply"); len(nulls) != 0 {
		t.Errorf("the 202 holds null at %v: %s", nulls, raw)
	}
	if odd := notSnakeCase(t, map[string]any{"reply": reply, "pending_approval": reply["pending_approval"], "deviation": waiting}); len(odd) != 0 {
		t.Errorf("fields not written as the other routes write theirs: %v", odd)
	}

	// Approved: the plan it was applied with, and the record as the ledger's
	// own route reads it.
	status, raw = h.send(t, h.secondAdministrator(t), requestPath(asked.PendingApproval.RequestID)+"/approve", "")
	if status != http.StatusOK {
		t.Fatalf("the second administrator approves: %d (%s)", status, raw)
	}
	replies = append(replies, raw)
	reply = map[string]any{}
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	requireFields(t, reply, "an approval", "request", "applied", "deviation", "plan")
	requireFields(t, reply["plan"], "an applied plan", planFields...)
	if reply["applied"] != true {
		t.Fatalf("the approval: %s", raw)
	}
	record := reply["deviation"].(map[string]any)
	for name, want := range map[string]any{"kind": "waive", "scope": "task", "origin": "in_place", "status": "applied",
		"node_id": "step", "node_name": "Check the order", "actor": "boss", "actor_is_server": false, "reason": routeReason,
		"approved_by": seconderName, "request_id": asked.PendingApproval.RequestID} {
		if record[name] != want {
			t.Errorf("the record's %s is %v, want %v", name, record[name], want)
		}
	}
	// How many places read the step is a count: a list in the record would
	// read as all of them.
	if details, _ := record["details"].(map[string]any); details["decision_points"] != float64(1) {
		t.Errorf("the record's details %v, want the number of decision points", record["details"])
	}
	_, ledger, ledgerRaw := h.readDeviations(t, admin, instanceID.String())
	replies = append(replies, ledgerRaw)
	if len(ledger.Deviations) != 1 {
		t.Fatalf("the ledger holds %d rows, want the waive", len(ledger.Deviations))
	}
	asRead, _ := json.Marshal(ledger.Deviations[0])
	asApplied, _ := json.Marshal(record)
	if string(asRead) != string(asApplied) {
		t.Errorf("the apply answered the record as\n%s\nand the ledger's route reads it as\n%s", asApplied, asRead)
	}
	if nulls := nullsIn(reply, "reply"); len(nulls) != 0 {
		t.Errorf("the approval holds null at %v: %s", nulls, raw)
	}
	if odd := notSnakeCase(t, map[string]any{"reply": reply, "request": reply["request"], "deviation": record}); len(odd) != 0 {
		t.Errorf("fields not written as the other routes write theirs: %v", odd)
	}

	// Replayed: the record again, and a plan that names the act and lists
	// nothing — what there was to list is no longer there to read. Its lists
	// are empty lists, and its counts say nothing of what there was.
	applied := h.everyRow(t)
	reply, raw = h.deviateObject(t, admin, instanceID, apply)
	requireFields(t, reply, "a replay", "plan", "applied", "replayed", "deviation")
	requireFields(t, reply["plan"], "a replayed plan", planFields...)
	if reply["applied"] != true || reply["replayed"] != true {
		t.Fatalf("the same apply again: %s", raw)
	}
	for _, empty := range []string{`"open_work":[]`, `"decision_points":[]`, `"missing":[]`, `"called_instances":[]`, `"refusals":[]`, `"warnings":[]`, `"outputs":{}`} {
		if !strings.Contains(raw, empty) {
			t.Errorf("the replayed plan has no %s: %s", empty, raw)
		}
	}
	if again, _ := json.Marshal(reply["deviation"]); string(again) != string(asApplied) {
		t.Errorf("the replay answered the record as\n%s\nwant it as the apply did\n%s", again, asApplied)
	}
	if nulls := nullsIn(reply, "reply"); len(nulls) != 0 {
		t.Errorf("the replay holds null at %v: %s", nulls, raw)
	}
	h.requireUnchanged(t, applied, "the same apply again")

	// Nobody's account id, in any of it: not the administrator's, who asked,
	// not the second administrator's, who approved, and not the holder's,
	// whose task was taken — in the preview that refused, the preview that did
	// not, the 202 of the request, the approval, the row as the ledger's route
	// reads it, and the replay.
	replies = append(replies, raw)
	if len(replies) != 6 {
		t.Fatalf("%d replies were kept to look through, want six", len(replies))
	}
	for who, account := range map[string]string{"the administrator": h.accountID(t, "boss").String(),
		"the second administrator": h.accountID(t, seconderName).String(), "the holder": h.accountID(t, "alice").String()} {
		for i, said := range replies {
			if strings.Contains(said, account) {
				t.Errorf("reply %d carries the account id of %s: %s", i+1, who, said)
			}
		}
	}
}

// Controller note 5. What a waive counts a number as is sent as JSON and read
// twice: by the gateway after the step, in the advance the waive makes, and
// by the comparison that tells the same request sent again from another one.
// The two agree: the gateway takes the branch the number fits, and the retry
// is answered with the record — never "already waived by".
//
// Between the two the number is kept: a waive waits in a request until a
// second administrator approves it, and what the approval sets is what the
// request stored. So this also pins that a number survives being kept in a
// request and read back at approval.
func TestANumberAWaiveCountsAsIsTheNumberTheGatewayCompares(t *testing.T) {
	h := newDeviationRouteHarness(t)
	admin := h.signIn(t, "boss", entities.RoleAdmin)
	request := func(amount, visitKey string) string {
		body := fmt.Sprintf(`{"kind":"waive","node_id":"step","reason":%q,"outputs":{"amount":%s}`, routeReason, amount)
		if visitKey != "" {
			body += fmt.Sprintf(`,"visit_key":%q,"dry_run":false`, visitKey)
		}
		return body + "}"
	}
	for _, c := range []struct{ amount, taken, notTaken string }{
		{"250", "large", "small"},
		{"100", "small", "large"},
		{"100.5", "large", "small"},
		{"1e2", "small", "large"},
	} {
		instanceID := h.start(t, orderBySize())
		status, planned, raw := h.deviateWith(t, admin, instanceID, request(c.amount, ""))
		if status != http.StatusOK || !planned.Plan.Applicable {
			t.Fatalf("the preview of amount %s: %d (%s)", c.amount, status, raw)
		}
		apply := request(c.amount, planned.Plan.VisitKey)
		status, applied, raw := h.secondedWith(t, admin, instanceID, apply)
		if status != http.StatusOK || !applied.Applied || applied.Replayed {
			t.Fatalf("the waive counted as amount %s, approved by a second administrator: %d (%s), want it applied", c.amount, status, raw)
		}
		if h.openTasksOn(t, instanceID, c.taken) != 1 || h.openTasksOn(t, instanceID, c.notTaken) != 0 {
			t.Fatalf("a waive counted as amount %s did not go to %q", c.amount, c.taken)
		}
		// The same request, byte for byte: its record, and nothing done again.
		before := h.everyRow(t)
		status, again, raw := h.deviateWith(t, admin, instanceID, apply)
		if status != http.StatusOK || !again.Applied || !again.Replayed || again.Deviation["id"] != applied.Deviation["id"] {
			t.Fatalf("the same waive of amount %s again: %d (%s), want a 200 replay of the record", c.amount, status, raw)
		}
		h.requireUnchanged(t, before, "the same waive again")
	}

	// The same number written another way is the same request; another value
	// is not, and neither is the number's digits as words.
	instanceID := h.start(t, orderBySize())
	_, planned, _ := h.deviateWith(t, admin, instanceID, request("250", ""))
	if status, applied, raw := h.secondedWith(t, admin, instanceID, request("250", planned.Plan.VisitKey)); status != http.StatusOK || !applied.Applied {
		t.Fatalf("the waive, approved by a second administrator: %d (%s)", status, raw)
	}
	before := h.everyRow(t)
	for _, same := range []string{"250.0", "2.5e2", " 250 "} {
		if status, again, raw := h.deviateWith(t, admin, instanceID, request(same, planned.Plan.VisitKey)); status != http.StatusOK || !again.Replayed {
			t.Errorf("the same waive with the amount written %s: %d (%s), want a 200 replay", same, status, raw)
		}
	}
	alreadyWaived := invalid("this step was already waived by boss")
	for _, other := range []string{"251", `"250"`, "250.5"} {
		if status, _, raw := h.deviateWith(t, admin, instanceID, request(other, planned.Plan.VisitKey)); status != http.StatusBadRequest || !sameJSON(t, raw, alreadyWaived) {
			t.Errorf("another waive of the same visit, counted as %s: %d (%s), want 400 %s", other, status, raw, alreadyWaived)
		}
	}
	h.requireUnchanged(t, before, "requests for a visit already waived")
	instance, err := h.svc.GetInstance(h.tenantContext(), instanceID)
	if err != nil || instance.Variables["amount"] != float64(250) {
		t.Fatalf("the instance holds amount = %v (%T, err %v), want the number 250", instance.Variables["amount"], instance.Variables["amount"], err)
	}
}

// strandOneStep deploys start → step, a step nothing follows, starts it and
// completes the step. The engine ends an instance only at an end event, so the
// instance is left active and holding no token: what docs/upgrading.md calls
// "with nothing left", reached the way production reaches it.
func (h *deviationHarness) strandOneStep(t *testing.T) uuid.UUID {
	t.Helper()
	id := h.start(t, &entities.ProcessDefinition{
		Key: "deviation-dead-end", Name: "Dead end",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			{ID: "step", Name: "Check the order", Type: entities.UserTask, Assignee: "alice", Incoming: []string{"f1"}},
		},
		Flows: []*entities.SequenceFlow{{ID: "f1", SourceRef: "start", TargetRef: "step"}},
	})
	h.completeStep(t, id)
	instance, err := h.svc.GetInstance(h.tenantContext(), id)
	if err != nil || instance.Status != entities.ProcessActive || len(instance.Tokens) != 0 {
		t.Fatalf("the fixture did not strand the instance: %s with %d token(s) (err %v)", instance.Status, len(instance.Tokens), err)
	}
	return id
}

// Rulings addendum §10. An instance that is active and waiting nowhere is
// closed by a cancel that names no step: the plan warns that it has nothing
// left to do, and neither the plan nor the record names a step.
func TestAnInstanceWithNothingLeftIsClosedOverTheRoute(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.strandOneStep(t)
	admin := h.signIn(t, "boss", entities.RoleAdmin)
	before := h.everyRow(t)

	body := map[string]any{"kind": "cancel", "reason": routeReason}
	status, planned, raw := h.deviate(t, admin, instanceID, body)
	if status != http.StatusOK || planned.Applied || !planned.Plan.Applicable || len(planned.Plan.Refusals) != 0 || planned.Plan.VisitKey == "" ||
		!slices.Equal(planned.Plan.Warnings, []string{"This instance is not waiting at any step. Cancelling it closes it."}) {
		t.Fatalf("the preview: %d (%s), want a 200 plan with no refusal and the warning that it waits nowhere", status, raw)
	}
	if strings.Contains(raw, `"node_id"`) || strings.Contains(raw, `"node_name"`) {
		t.Fatalf("the plan names a step the request did not: %s", raw)
	}
	h.requireUnchanged(t, before, "the preview")

	// A waive and a hold act on a step; with none they are malformed, on this
	// instance as on any.
	for _, kind := range []string{"waive", "hold"} {
		want := invalid("say which step: node_id is required for a waive and a hold")
		if status, _, raw := h.deviate(t, admin, instanceID, map[string]any{"kind": kind, "reason": routeReason}); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
			t.Errorf("a %s that names no step: %d (%s), want 400 %s", kind, status, raw, want)
		}
	}

	body["visit_key"], body["dry_run"] = planned.Plan.VisitKey, false
	status, applied, raw := h.deviate(t, admin, instanceID, body)
	if status != http.StatusOK || !applied.Applied || applied.Replayed || applied.Deviation["kind"] != "cancel" ||
		applied.Deviation["scope"] != "instance" || applied.Deviation["actor"] != "boss" {
		t.Fatalf("the apply: %d (%s)", status, raw)
	}
	if strings.Contains(raw, `"node_id"`) || strings.Contains(raw, `"node_name"`) {
		t.Fatalf("the reply names a step the instance was not at: %s", raw)
	}
	instance, err := h.svc.GetInstance(h.tenantContext(), instanceID)
	if err != nil || instance.Status != entities.ProcessCancelled {
		t.Fatalf("the instance is %s (err %v), want cancelled", instance.Status, err)
	}
}
