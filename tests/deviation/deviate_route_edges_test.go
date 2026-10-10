package deviation_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// sendKeyed posts body under an Idempotency-Key and answers the status, the
// reply and whether the header's own check said it was answering again.
func (h *deviationHarness) sendKeyed(t *testing.T, token string, instanceID uuid.UUID, key, body string) (status int, raw string, replayed bool) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, h.server.URL+deviationsPath(instanceID), strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", key)
	resp, err := routeClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the reply: %v", err)
	}
	return resp.StatusCode, strings.TrimSpace(string(out)), resp.Header.Get("Idempotency-Replayed") == "true"
}

// The route is safe to retry without an Idempotency-Key: the visit key does
// that. A client that sends one anyway meets the header's own rules, which are
// not the route's, and the documentation says what they are. This is them,
// run on this route.
func TestAnIdempotencyKeyOnADeviationFollowsTheHeadersOwnRules(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	admin := h.signIn(t, "boss", entities.RoleAdmin)
	preview := fmt.Sprintf(`{"kind":"hold","node_id":"step","reason":%q}`, routeReason)

	// A preview under a key, and the same again: the first answer, returned.
	status, first, replayed := h.sendKeyed(t, admin, instanceID, "one-key", preview)
	if status != http.StatusOK || replayed || !strings.Contains(first, `"applied":false`) {
		t.Fatalf("a preview under a key: %d, replayed by the header %v (%s)", status, replayed, first)
	}
	status, again, replayed := h.sendKeyed(t, admin, instanceID, "one-key", preview)
	if status != http.StatusOK || !replayed || again != first {
		t.Fatalf("the same preview under the same key: %d, replayed by the header %v\n%s\nwant the first answer again\n%s", status, replayed, again, first)
	}

	// Its apply is another body: under the same key it is refused by the
	// header's check, in plain text, before the route sees it.
	apply, _ := h.previewed(t, admin, instanceID, map[string]any{"kind": "hold", "node_id": "step", "reason": routeReason})
	applyBody := fmt.Sprintf(`{"kind":"hold","node_id":"step","reason":%q,"visit_key":%q,"dry_run":false}`, routeReason, apply["visit_key"])
	status, raw, _ := h.sendKeyed(t, admin, instanceID, "one-key", applyBody)
	if status != http.StatusConflict || strings.HasPrefix(raw, "{") {
		t.Fatalf("an apply under its preview's key: %d (%s), want the header's own 409, in plain text", status, raw)
	}
	if rows := h.rowCount(t, instanceID); rows != 0 {
		t.Fatalf("an apply refused by the header's check wrote %d ledger row(s)", rows)
	}

	// Under a key of its own it is made, once. Sent again under that key the
	// first answer comes back whole — so its "replayed" still reads false,
	// and the header is what says it is a repeat.
	status, applied, replayed := h.sendKeyed(t, admin, instanceID, "another-key", applyBody)
	if status != http.StatusOK || replayed || !strings.Contains(applied, `"applied":true`) || !strings.Contains(applied, `"replayed":false`) {
		t.Fatalf("the apply under a key of its own: %d, replayed by the header %v (%s)", status, replayed, applied)
	}
	held := h.everyRow(t)
	status, repeated, replayed := h.sendKeyed(t, admin, instanceID, "another-key", applyBody)
	if status != http.StatusOK || !replayed || repeated != applied {
		t.Fatalf("the same apply under the same key: %d, replayed by the header %v\n%s\nwant the first answer again\n%s", status, replayed, repeated, applied)
	}
	// Under a third key the route itself answers: its own replay.
	status, byTheRoute, replayed := h.sendKeyed(t, admin, instanceID, "a-third-key", applyBody)
	if status != http.StatusOK || replayed || !strings.Contains(byTheRoute, `"replayed":true`) {
		t.Fatalf("the same apply under a new key: %d, replayed by the header %v (%s), want the route's own replay", status, replayed, byTheRoute)
	}
	if rows := h.rowCount(t, instanceID); rows != 1 {
		t.Fatalf("the ledger holds %d rows after one hold sent three times, want one", rows)
	}
	// The idempotency store keeps each first answer, and nothing else changed.
	for table, rows := range h.everyRow(t) {
		if held[table] != rows && table != "idempotency_records" {
			t.Errorf("sending the apply again changed %s (%s, was %s)", table, rows, held[table])
		}
	}

	// A refusal is kept too: the first answer under a key is returned again
	// whatever its status.
	status, refused, _ := h.sendKeyed(t, admin, instanceID, "a-bad-key", `{"kind":"skip","reason":"x"}`)
	status2, refusedAgain, replayed := h.sendKeyed(t, admin, instanceID, "a-bad-key", `{"kind":"skip","reason":"x"}`)
	if status != http.StatusBadRequest || status2 != http.StatusBadRequest || !replayed || refused != refusedAgain {
		t.Errorf("a malformed request under a key, twice: %d (%s) then %d, replayed by the header %v (%s)", status, refused, status2, replayed, refusedAgain)
	}
}

// What a request's Content-Type says is not what decides how it is read: the
// body is read as JSON whatever the header says, as on every route. Access is
// by a bearer token, which a page in somebody's browser cannot send for them,
// so nothing rests on the header.
func TestADeviationIsReadAsJSONWhateverItsContentTypeSays(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	admin := h.signIn(t, "boss", entities.RoleAdmin)
	apply, _ := h.previewed(t, admin, instanceID, map[string]any{"kind": "hold", "node_id": "step", "reason": routeReason})
	before := h.everyRow(t)

	preview := fmt.Sprintf(`{"kind":"hold","node_id":"step","reason":%q}`, routeReason)
	for _, contentType := range []string{"text/plain", "application/x-www-form-urlencoded", ""} {
		status, raw := h.send(t, admin, deviationsPath(instanceID), preview, "Content-Type", contentType)
		if status != http.StatusOK || !strings.Contains(raw, `"applied":false`) {
			t.Errorf("a preview sent as %q: %d (%s), want a 200 plan", contentType, status, raw)
		}
		// What is not JSON is refused as that, whatever it says it is.
		status, raw = h.send(t, admin, deviationsPath(instanceID), "kind=hold&node_id=step&dry_run=false", "Content-Type", contentType)
		if status != http.StatusBadRequest || !sameJSON(t, raw, invalid(requestCouldNotBeRead)) {
			t.Errorf("a form sent as %q: %d (%s), want 400 %s", contentType, status, raw, invalid(requestCouldNotBeRead))
		}
	}
	h.requireUnchanged(t, before, "previews sent under another Content-Type")

	applyBody := fmt.Sprintf(`{"kind":"hold","node_id":"step","reason":%q,"visit_key":%q,"dry_run":false}`, routeReason, apply["visit_key"])
	if status, raw := h.send(t, admin, deviationsPath(instanceID), applyBody, "Content-Type", "text/plain"); status != http.StatusOK || !strings.Contains(raw, `"applied":true`) {
		t.Errorf("an apply sent as text/plain: %d (%s), want it applied as any other", status, raw)
	}
}

// Every number in outputs is read as a float, as a task completion's are. Two
// whole numbers past 2^53 that round to the same float are therefore one
// request: the second is answered with the first's record. A large identifier
// is sent as a string.
//
// The waive waits in a request until a second administrator approves it, so
// this also pins that a number past a float's precision is the same number
// once it has been kept in a request and read back.
func TestTwoWholeNumbersPastTheFloatsPrecisionAreOneRequest(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.start(t, orderBySize())
	admin := h.signIn(t, "boss", entities.RoleAdmin)
	request := func(amount, visitKey string) string {
		body := fmt.Sprintf(`{"kind":"waive","node_id":"step","reason":%q,"outputs":{"amount":%s}`, routeReason, amount)
		if visitKey != "" {
			body += fmt.Sprintf(`,"visit_key":%q,"dry_run":false`, visitKey)
		}
		return body + "}"
	}
	// 2^53 + 1 is not a float; it is read as 2^53.
	const pastPrecision, itsFloat = "9007199254740993", "9007199254740992"
	status, planned, raw := h.deviateWith(t, admin, instanceID, request(pastPrecision, ""))
	if status != http.StatusOK || !planned.Plan.Applicable {
		t.Fatalf("the preview: %d (%s)", status, raw)
	}
	status, applied, raw := h.secondedWith(t, admin, instanceID, request(pastPrecision, planned.Plan.VisitKey))
	if status != http.StatusOK || !applied.Applied {
		t.Fatalf("the waive, approved by a second administrator: %d (%s), want it applied", status, raw)
	}
	before := h.everyRow(t)
	for _, same := range []string{pastPrecision, itsFloat} {
		status, again, raw := h.deviateWith(t, admin, instanceID, request(same, planned.Plan.VisitKey))
		if status != http.StatusOK || !again.Replayed || again.Deviation["id"] != applied.Deviation["id"] {
			t.Errorf("the same waive with the amount written %s: %d (%s), want a 200 replay of the record", same, status, raw)
		}
	}
	// The next float up is another value.
	alreadyWaived := invalid("this step was already waived by boss")
	if status, _, raw := h.deviateWith(t, admin, instanceID, request("9007199254740994", planned.Plan.VisitKey)); status != http.StatusBadRequest || !sameJSON(t, raw, alreadyWaived) {
		t.Errorf("another waive of the same visit, counted as the next float: %d (%s), want 400 %s", status, raw, alreadyWaived)
	}
	h.requireUnchanged(t, before, "requests for a visit already waived")
	instance, err := h.svc.GetInstance(h.tenantContext(), instanceID)
	if err != nil || instance.Variables["amount"] != float64(9007199254740992) {
		t.Fatalf("the instance holds amount = %v (%T, err %v), want the float 2^53", instance.Variables["amount"], instance.Variables["amount"], err)
	}
}
