package deviation_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

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
		"a request named in a body":   `{"reason":"x","request_id":"` + requestID + `"}`,
		"self_approved":               `{"reason":"x","self_approved":true}`,
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
