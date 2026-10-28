package webhook_test

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/webhooksig"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// Simulating a delivery is how a webhook is set up without asking the partner
// to send again after every change. It takes every step a real delivery takes
// and does none of them: the instance it names is still waiting afterwards,
// and the delivery ID it was given is still free for the real one.

func TestASimulatedDeliveryNamesWhatItWouldMoveAndMovesNothing(t *testing.T) {
	h := newWebhookHarness(t)
	instanceID := h.waitForPayment(t, "order-1")
	hook := h.register(t, "order.paid", "order.id")
	body := []byte(`{"order":{"id":"order-1"}}`)

	result := h.simulate(t, hook, signedSimulation(hook.Secret, body, "evt-1"))

	if !result.WouldAct || result.Refusal != "" {
		t.Fatalf("a signed delivery for a waiting order would not act: %+v", result)
	}
	if !result.Signature.Checked || !result.Signature.Valid {
		t.Errorf("the signature was not checked as valid: %+v", result.Signature)
	}
	if result.CorrelationKey != "order-1" {
		t.Errorf("correlation key = %q, want order-1", result.CorrelationKey)
	}
	if result.WaitingTotal != 1 || len(result.Waiting) != 1 {
		t.Fatalf("waiting = %d (%d named), want the one instance", result.WaitingTotal, len(result.Waiting))
	}
	waiting := result.Waiting[0]
	if waiting.InstanceID != instanceID || waiting.NodeID != "await-payment" || waiting.NodeName != "Wait for payment" {
		t.Errorf("named %+v, want instance %s at Wait for payment", waiting, instanceID)
	}
	if waiting.ProcessKey != "order-payment" || waiting.Since.IsZero() {
		t.Errorf("named %+v, want process order-payment and when it began waiting", waiting)
	}

	// Nothing moved.
	subs, err := h.repo.Subscription().FindMessages(h.ctx, h.projectID, "order.paid", "order-1")
	if err != nil {
		t.Fatalf("find subscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("after a simulation the instance has %d subscriptions, want it still waiting", len(subs))
	}

	// And the delivery ID was not taken: the real delivery is the first.
	outcome, err := h.deliver(t, hook, body, "evt-1")
	if err != nil {
		t.Fatalf("the real delivery after a simulation: %v", err)
	}
	if outcome.Duplicate {
		t.Fatal("the simulation claimed the delivery ID, so the real delivery was dropped as a duplicate")
	}
	subs, err = h.repo.Subscription().FindMessages(h.ctx, h.projectID, "order.paid", "order-1")
	if err != nil {
		t.Fatalf("find subscriptions: %v", err)
	}
	if len(subs) != 0 {
		t.Error("the real delivery did not move the instance the simulation named")
	}
}

// The answer says the signature is wrong and how to make it right. It never
// says what it should have been: that would sign any body for anybody allowed
// to simulate, who could then post it to the public address as the partner.
func TestASimulatedSignatureThatDoesNotMatchIsExplainedButNeverSupplied(t *testing.T) {
	h := newWebhookHarness(t)
	hook := h.register(t, "order.paid", "order.id")
	body := []byte(`{"order":{"id":"order-1"}}`)
	sim := signedSimulation("not the secret", body, "evt-1")

	result := h.simulate(t, hook, sim)

	if !result.Signature.Checked || result.Signature.Valid || result.Signature.Problem == "" {
		t.Fatalf("a wrong signature was not reported as one: %+v", result.Signature)
	}
	if result.WouldAct || result.Refusal != result.Signature.Problem {
		t.Errorf("a wrongly signed delivery would act, or the refusal is not the signature's: %+v", result)
	}
	// The body is still read, because that is the next thing to fix.
	if result.CorrelationKey != "order-1" {
		t.Errorf("the body was not read past the bad signature: key %q", result.CorrelationKey)
	}

	answer, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("encode the result: %v", err)
	}
	expected := webhooksig.SignV2(body, hook.Secret, sim.Timestamp, sim.DeliveryID)
	digest := strings.TrimPrefix(expected, "v2=")
	for name, leak := range map[string]string{"the signature it should have had": digest, "the secret": hook.Secret} {
		if strings.Contains(string(answer), leak) {
			t.Errorf("the simulation gave back %s: %s", name, answer)
		}
	}
}

// Each refusal a real delivery would meet is said in the words its sender
// would be answered with, and the simulation says it would not act.
func TestASimulationSaysWhyARealDeliveryWouldBeRefused(t *testing.T) {
	h := newWebhookHarness(t)
	hook := h.register(t, "order.paid", "order.id")

	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"no key":        {`{"order":{}}`, "order.id"},
		"empty key":     {`{"order":{"id":""}}`, "empty"},
		"not an object": {`[1,2,3]`, "JSON object"},
	} {
		result := h.simulate(t, hook, entities.WebhookSimulation{Body: []byte(tc.body)})
		if result.WouldAct || !strings.Contains(result.Refusal, tc.want) || result.BodyProblem != result.Refusal {
			t.Errorf("%s: refusal %q, would act %v; want one naming %q", name, result.Refusal, result.WouldAct, tc.want)
		}
		if strings.HasPrefix(result.Refusal, "webhook:") {
			t.Errorf("%s: the refusal kept the log's prefix: %q", name, result.Refusal)
		}
	}

	h.disable(t, hook.ID)
	result := h.simulate(t, hook, entities.WebhookSimulation{Body: []byte(`{"order":{"id":"order-1"}}`)})
	if result.WouldAct || result.Enabled || !strings.Contains(result.Refusal, "switched off") {
		t.Errorf("a switched-off webhook: %+v", result)
	}
}

// A delivery whose ID has already been acted on would be answered as a
// duplicate. Telling the person that saves them looking for a bug in their
// process when the sender is resending an old event.
func TestASimulationRecognisesADeliveryAlreadyReceived(t *testing.T) {
	h := newWebhookHarness(t)
	h.waitForPayment(t, "order-1")
	h.waitForPayment(t, "order-2")
	hook := h.register(t, "order.paid", "order.id")
	if _, err := h.deliver(t, hook, []byte(`{"order":{"id":"order-1"}}`), "evt-1"); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	body := []byte(`{"order":{"id":"order-2"}}`)
	result := h.simulate(t, hook, signedSimulation(hook.Secret, body, "evt-1"))
	if !result.AlreadyReceived || result.WouldAct {
		t.Errorf("a delivery ID already acted on was not recognised: %+v", result)
	}

	result = h.simulate(t, hook, signedSimulation(hook.Secret, body, "evt-2"))
	if result.AlreadyReceived || !result.WouldAct {
		t.Errorf("a new delivery ID was taken for one already received: %+v", result)
	}
}

// A webhook with no correlation starts the processes that listen for its
// message, and the simulation names them.
func TestASimulationOfAnUncorrelatedWebhookNamesWhatItWouldStart(t *testing.T) {
	h := newWebhookHarness(t)
	h.deploy(t, &entities.ProcessDefinition{
		Key:  "intake",
		Name: "Order intake",
		Nodes: []*entities.Node{
			{ID: "received", Type: entities.StartEvent, Name: "Order received", Properties: map[string]any{"message_name": "order.received"}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{{ID: "f1", SourceRef: "received", TargetRef: "end"}},
	})
	hook := h.register(t, "order.received", "")

	result := h.simulate(t, hook, entities.WebhookSimulation{Body: []byte(`{"anything":"at all"}`)})
	if !result.WouldAct || len(result.Starts) != 1 {
		t.Fatalf("an uncorrelated webhook would start %d processes: %+v", len(result.Starts), result)
	}
	if start := result.Starts[0]; start.ProcessKey != "intake" || start.NodeName != "Order received" {
		t.Errorf("named %+v, want intake at Order received", start)
	}
	if result.Signature.Checked {
		t.Error("a simulation with no signature says it checked one")
	}
}

// Nothing waiting is not a refusal: a real delivery is accepted and changes
// nothing, and the simulation says it would not act.
func TestASimulationWithNothingWaitingWouldNotAct(t *testing.T) {
	h := newWebhookHarness(t)
	hook := h.register(t, "order.paid", "order.id")

	result := h.simulate(t, hook, entities.WebhookSimulation{Body: []byte(`{"order":{"id":"order-9"}}`)})
	if result.WouldAct || result.Refusal != "" || result.WaitingTotal != 0 {
		t.Errorf("with nothing waiting: %+v", result)
	}
}

// Another organisation's webhook is not found, the same answer as one that
// does not exist: its secret checks no signature for a stranger, and its
// waiting instances are not named to one.
func TestAnotherOrganisationsWebhookCannotBeSimulated(t *testing.T) {
	h := newWebhookHarness(t)
	hook := h.register(t, "order.paid", "order.id")

	other, err := serviceimpl.NewOrganizationService(h.repo, nil).CreateOrganization(h.ctx, "Other Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	strangerCtx := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: other.ID.String()})

	_, err = h.service.SimulateWebhookDelivery(strangerCtx, hook.ID, entities.WebhookSimulation{Body: []byte(`{}`)})
	if !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("another organisation simulated this webhook: err = %v, want not found", err)
	}
}

// A body larger than a real delivery may be is refused before anything else.
func TestAnOversizedSimulationIsRefused(t *testing.T) {
	h := newWebhookHarness(t)
	hook := h.register(t, "order.paid", "")
	_, err := h.service.SimulateWebhookDelivery(h.ctx, hook.ID, entities.WebhookSimulation{Body: make([]byte, 1<<20+1)})
	if !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("err = %v, want an invalid argument", err)
	}
}

// helpers

func (h *webhookHarness) simulate(t *testing.T, hook entities.Webhook, sim entities.WebhookSimulation) entities.WebhookSimulationResult {
	t.Helper()
	result, err := h.service.SimulateWebhookDelivery(h.ctx, hook.ID, sim)
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	return result
}

// signedSimulation is what a partner would send now, signed with secret.
func signedSimulation(secret string, body []byte, deliveryID string) entities.WebhookSimulation {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	return entities.WebhookSimulation{
		Body:       body,
		Timestamp:  timestamp,
		DeliveryID: deliveryID,
		Signature:  webhooksig.SignV2(body, secret, timestamp, deliveryID),
	}
}

// waitForPayment starts an order that waits for order.paid, correlated on its
// orderId.
func (h *webhookHarness) waitForPayment(t *testing.T, orderID string) uuid.UUID {
	t.Helper()
	h.deploy(t, &entities.ProcessDefinition{
		Key:  "order-payment",
		Name: "Order payment",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "await-payment", Type: entities.IntermediateCatchEvent, Name: "Wait for payment", Properties: map[string]any{
				"message_name":    "order.paid",
				"correlation_key": "${orderId}",
			}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "await-payment"},
			{ID: "f2", SourceRef: "await-payment", TargetRef: "end"},
		},
	})
	id, err := h.engine.StartProcess(h.ctx, h.projectID, "order-payment", map[string]any{"orderId": orderID})
	if err != nil {
		t.Fatalf("start an order: %v", err)
	}
	return id
}

// deploy saves a definition once, however many times a test asks for it.
func (h *webhookHarness) deploy(t *testing.T, def *entities.ProcessDefinition) {
	t.Helper()
	if _, err := h.repo.Definition().GetLiveByProjectKey(h.ctx, h.projectID, def.Key); err == nil {
		return
	}
	def.Project = &entities.Project{ID: h.projectID}
	if _, err := serviceimpl.NewDefinitionService(h.repo).CreateDefinition(h.ctx, def); err != nil {
		t.Fatalf("create definition %s: %v", def.Key, err)
	}
}
