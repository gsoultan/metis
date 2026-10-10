package deviation_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/tests/testutils"
)

const waiveUndone = "so the waive was not applied and nothing was changed."

// refusedByAGateway applies a waive whose plan does not refuse it, and
// requires the 400 that names the gateway the advance could not get past, with
// every table as it was: the waive is undone whole.
func (h *deviationHarness) refusedByAGateway(t *testing.T, admin string, instanceID uuid.UUID, body map[string]any, sentence string) {
	t.Helper()
	apply, planned := h.previewed(t, admin, instanceID, body)
	if !planned.Plan.Applicable {
		t.Fatalf("the plan refuses, so the advance is never tried and this proves nothing: %q", planned.Plan.Refusals)
	}
	before := h.everyRow(t)
	if status, _, raw := h.deviate(t, admin, instanceID, apply); status != http.StatusBadRequest || !sameJSON(t, raw, invalid(sentence)) {
		t.Fatalf("a waive the process could not follow: %d (%s), want 400 %s", status, raw, invalid(sentence))
	}
	h.requireUnchanged(t, before, "a waive a gateway had no way out for")
}

// Task 6 F6-1, F6-6. A waive whose advance reaches a gateway with no way out
// is the caller's to hear about, in words that say whose gateway it was and —
// only where they can — what to give. Each is a 400, and the waive is undone
// whole.
func TestAWaiveAGatewayCannotFollowIsA400ThatSaysWhoseGatewayItWas(t *testing.T) {
	h := newDeviationRouteHarness(t)
	admin := h.signIn(t, "boss", entities.RoleAdmin)

	// This instance's gateway, and a value was given: say one it accepts.
	given := h.start(t, orderBySize())
	h.refusedByAGateway(t, admin, given,
		map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason, "outputs": map[string]any{"amount": -5}},
		"The values given fit no way out of “Large order?”, "+waiveUndone+" Preview again and give a value one of its branches accepts.")
	if !h.stepIsOpen(t, given) {
		t.Fatal("the step is not open again after the waive was undone")
	}

	// This instance's gateway, and nothing was given: the step's form has no
	// field the gateway reads, so there is nothing of the caller's that fitted
	// badly.
	undeclared := orderBySize()
	undeclared.Nodes[1].Properties = nil
	h.refusedByAGateway(t, admin, h.start(t, undeclared),
		map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason},
		"“Large order?” had no way out for the values this instance holds, "+waiveUndone)

	// The gateway of the process that started this one.
	called := h.deploy(t, &entities.ProcessDefinition{
		Key: "deviation-supplier-review", Name: "Supplier review",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "step", Type: entities.UserTask, Name: "Review the supplier", Assignee: "alice", Properties: testutils.FormDeclaring("approved")},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "c1", SourceRef: "start", TargetRef: "step"},
			{ID: "c2", SourceRef: "step", TargetRef: "end"},
		},
	})
	caller := h.start(t, &entities.ProcessDefinition{
		Key: "deviation-supplier-onboarding", Name: "Supplier onboarding",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "check", Type: entities.CallActivity, Name: "Check the supplier", Properties: map[string]any{"called_process_key": called}},
			{ID: "decide", Type: entities.ExclusiveGateway, Name: "Supplier approved?"},
			{ID: "sign", Type: entities.UserTask, Name: "Sign the contract"},
			{ID: "drop", Type: entities.UserTask, Name: "Drop the supplier"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "p1", SourceRef: "start", TargetRef: "check"},
			{ID: "p2", SourceRef: "check", TargetRef: "decide"},
			{ID: "yes", SourceRef: "decide", TargetRef: "sign", Condition: "approved"},
			{ID: "no", SourceRef: "decide", TargetRef: "drop", Condition: "approved = false"},
			{ID: "p3", SourceRef: "sign", TargetRef: "end"},
			{ID: "p4", SourceRef: "drop", TargetRef: "end"},
		},
	})
	children, err := h.svc.ListSubProcesses(h.tenantContext(), caller)
	if err != nil || len(children) != 1 {
		t.Fatalf("the caller started %d process(es) (err %v), want the one it calls", len(children), err)
	}
	h.refusedByAGateway(t, admin, children[0].ID,
		map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason},
		"“Supplier approved?”, in the process that started this one, had no way out for the result, "+waiveUndone)

	// The gateway of a process the step after the waived one calls.
	stock := h.deploy(t, &entities.ProcessDefinition{
		Key: "deviation-stock-check", Name: "Stock check",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "in-hand", Type: entities.ExclusiveGateway, Name: "Stock in hand?"},
			{ID: "ship", Type: entities.UserTask, Name: "Ship it"},
			{ID: "order", Type: entities.UserTask, Name: "Order it"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "c1", SourceRef: "start", TargetRef: "in-hand"},
			{ID: "yes", SourceRef: "in-hand", TargetRef: "ship", Condition: "stock = plenty"},
			{ID: "no", SourceRef: "in-hand", TargetRef: "order", Condition: "stock = none"},
			{ID: "c2", SourceRef: "ship", TargetRef: "end"},
			{ID: "c3", SourceRef: "order", TargetRef: "end"},
		},
	})
	counted := h.start(t, &entities.ProcessDefinition{
		Key: "deviation-order-with-stock-check", Name: "Order",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "step", Type: entities.UserTask, Name: "Count the stock", Assignee: "alice", Properties: testutils.FormDeclaring("stock")},
			{ID: "check", Type: entities.CallActivity, Name: "Check the stock", Properties: map[string]any{"called_process_key": stock}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "step"},
			{ID: "f2", SourceRef: "step", TargetRef: "check"},
			{ID: "f3", SourceRef: "check", TargetRef: "end"},
		},
	})
	h.refusedByAGateway(t, admin, counted,
		map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason, "outputs": map[string]any{"stock": "some"}},
		"“Stock in hand?”, in another process this waive reached, had no way out, "+waiveUndone)
}
