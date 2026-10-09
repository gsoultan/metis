package deviation_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A request whose stored plan no longer opens is still a request somebody has
// to be able to see and end. Read by itself it answers 200 with what does
// open, the plan left out and named as unavailable — never an empty plan,
// which would say the requester was shown nothing. Approving it is the
// server's failure and changes nothing: an approval is of what was asked, and
// that has to be read whole. Rejecting it is answered as any rejection is, by
// a second administrator and by its requester.
func TestARequestWhoseStoredPlanNoLongerOpensIsReadAndRejectedOverTheAPI(t *testing.T) {
	h := newDeviationRouteHarness(t)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	for who, rejects := range map[string]string{"a second administrator": deputy, "its requester": boss} {
		t.Run("rejected by "+who, func(t *testing.T) {
			requestID := h.askToWaive(t, boss, h.oneStep(t))
			h.breakThePlanOf(t, uuid.MustParse(requestID))

			logs := captureLogs(t)
			status, read, raw := h.readRequest(t, deputy, requestID)
			if status != http.StatusOK || read.Request.ID != requestID || read.Request.Status != "pending_approval" || read.Request.RequestedBy != "boss" {
				t.Fatalf("reading a request whose plan no longer opens: %d (%s), want 200 and the request", status, raw)
			}
			// Why it did not open is said in the server's log, where whoever
			// operates the installation reads it: to the client the plan is
			// only "unavailable", and a lost key would show as nothing else.
			lines := logs.said("could not be opened")
			if len(lines) != 1 || lines[0]["level"] != "warn" || lines[0]["request"] != requestID || lines[0]["document"] != "plan" || lines[0]["error"] == "" {
				t.Fatalf("the read of a request whose plan no longer opens logged %v; want one warning naming the request, the plan, and why", lines)
			}
			written := object(t, raw)["request"].(map[string]any)
			if unavailable, _ := written["unavailable"].([]any); len(unavailable) != 1 || unavailable[0] != "plan" {
				t.Fatalf("the request says %v is unavailable, want the plan and nothing else: %s", written["unavailable"], raw)
			}
			if _, has := written["plan"]; has {
				t.Fatalf("a plan that could not be read is written all the same: %s", raw)
			}
			if _, has := written["command"]; !has {
				t.Fatalf("the command, which opens, is left out: %s", raw)
			}

			before := h.everyRow(t)
			if status, _, raw := h.decide(t, deputy, requestID, "approve", ""); status != http.StatusInternalServerError {
				t.Fatalf("approving a request whose plan no longer opens: %d (%s), want 500", status, raw)
			}
			h.requireUnchanged(t, before, "an approval of a request that cannot be read whole")
			if stored := h.requestStatus(t, requestID); stored != "pending_approval" {
				t.Fatalf("after the refused approval the request is stored as %q, want it still waiting", stored)
			}

			status, rejected, raw := h.decide(t, rejects, requestID, "reject", "its plan cannot be read")
			if status != http.StatusOK || rejected.Request.ID != requestID || rejected.Request.Status != "rejected" ||
				rejected.Request.DecisionReason != "its plan cannot be read" {
				t.Fatalf("rejecting it: %d (%s), want 200 and the request rejected", status, raw)
			}
			if stored := h.requestStatus(t, requestID); stored != "rejected" {
				t.Fatalf("the request is stored as %q, want rejected", stored)
			}
			// The rejection's answer names what the single read names: the
			// one document that does not open, and no other.
			answered := object(t, raw)["request"].(map[string]any)
			if unavailable, _ := answered["unavailable"].([]any); len(unavailable) != 1 || unavailable[0] != "plan" {
				t.Fatalf("the rejection says %v is unavailable, want the plan and nothing else: %s", answered["unavailable"], raw)
			}
			if _, has := answered["command"]; !has {
				t.Fatalf("the rejection leaves out the command, which opens: %s", raw)
			}
		})
	}
}
