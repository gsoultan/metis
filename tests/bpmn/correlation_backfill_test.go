package bpmn_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	service_impl2 "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

// The upgrade path for a process that is already waiting.
//
// Before correlation keys were resolved per instance, a message subscription
// stored the template text verbatim. Deploying the fix without repairing those
// rows would strand every instance already parked on a message catch event:
// inbound messages carry a real value ("order-1") and the stored key is still
// "${orderId}", so nothing ever matches and the instance waits forever.
//
// This walks the whole upgrade: park an instance, put its subscription back into
// the pre-fix shape, confirm it really is stranded, run the backfill, and
// confirm the message now reaches it.
func TestCorrelationBackfillRescuesStrandedInstances(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Backfill Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "order-payment-legacy",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "await-payment", Type: entities.IntermediateCatchEvent, Properties: map[string]any{
				"message_name":    "PaymentReceived",
				"correlation_key": "${orderId}",
			}},
			{ID: "confirm", Type: entities.UserTask, Name: "Confirm Order"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "await-payment"},
			{ID: "f2", SourceRef: "await-payment", TargetRef: "confirm"},
			{ID: "f3", SourceRef: "confirm", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "order-payment-legacy", map[string]any{"orderId": "order-1"})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	// Put the subscription back into the shape the old code wrote: the raw
	// template rather than this instance's resolved value.
	subs, err := h.repo.Subscription().ListByInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected exactly one subscription, got %d", len(subs))
	}
	if err := h.repo.Subscription().UpdateCorrelationKey(ctx, uuid.UUID(subs[0].ID), "${orderId}"); err != nil {
		t.Fatalf("stage the legacy correlation key: %v", err)
	}

	// Pre-upgrade state: the payment arrives and reaches nobody.
	if err := h.engine.SendMessage(ctx, h.projID, "PaymentReceived", "order-1", map[string]any{"amount": 4200}); err != nil {
		t.Fatalf("send message: %v", err)
	}
	if h.waitingAt(ctx, t, instanceID, "confirm") {
		t.Fatal("the legacy subscription was staged wrong — the instance resumed before the backfill ran")
	}

	// The migration.
	result, err := service_impl2.BackfillMessageCorrelationKeys(ctx, h.repo)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if result.Scanned != 1 || result.Rewritten != 1 || result.Unresolved != 0 {
		t.Errorf("expected 1 scanned / 1 rewritten / 0 unresolved, got %+v", result)
	}

	// Post-upgrade: the same message now correlates.
	if err := h.engine.SendMessage(ctx, h.projID, "PaymentReceived", "order-1", map[string]any{"amount": 4200}); err != nil {
		t.Fatalf("send message after backfill: %v", err)
	}
	if !h.waitingAt(ctx, t, instanceID, "confirm") {
		t.Error("the stranded instance was not rescued by the backfill")
	}
}

// Running the backfill twice must not corrupt an already-resolved key. A
// rewritten key contains no "${", so the second run has nothing to do.
func TestCorrelationBackfillIsIdempotent(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Backfill Idempotent Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "order-payment-idem",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "await-payment", Type: entities.IntermediateCatchEvent, Properties: map[string]any{
				"message_name":    "PaymentReceived",
				"correlation_key": "${orderId}",
			}},
			{ID: "confirm", Type: entities.UserTask, Name: "Confirm Order"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "await-payment"},
			{ID: "f2", SourceRef: "await-payment", TargetRef: "confirm"},
			{ID: "f3", SourceRef: "confirm", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "order-payment-idem", map[string]any{"orderId": "order-7"})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	subs, err := h.repo.Subscription().ListByInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected exactly one subscription, got %d", len(subs))
	}

	// A subscription written by the fixed code already holds the resolved value,
	// so a backfill run should not select it at all.
	first, err := service_impl2.BackfillMessageCorrelationKeys(ctx, h.repo)
	if err != nil {
		t.Fatalf("first backfill: %v", err)
	}
	if first.Scanned != 0 {
		t.Errorf("a correctly-resolved subscription was selected for backfill: %+v", first)
	}

	if err := h.repo.Subscription().UpdateCorrelationKey(ctx, uuid.UUID(subs[0].ID), "${orderId}"); err != nil {
		t.Fatalf("stage the legacy correlation key: %v", err)
	}

	second, err := service_impl2.BackfillMessageCorrelationKeys(ctx, h.repo)
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if second.Rewritten != 1 {
		t.Fatalf("expected the staged legacy key to be rewritten, got %+v", second)
	}

	third, err := service_impl2.BackfillMessageCorrelationKeys(ctx, h.repo)
	if err != nil {
		t.Fatalf("third backfill: %v", err)
	}
	if third.Scanned != 0 || third.Rewritten != 0 {
		t.Errorf("a second run over already-resolved keys did work it should not have: %+v", third)
	}

	if err := h.engine.SendMessage(ctx, h.projID, "PaymentReceived", "order-7", nil); err != nil {
		t.Fatalf("send message: %v", err)
	}
	if !h.waitingAt(ctx, t, instanceID, "confirm") {
		t.Error("the instance did not resume after a repeated backfill")
	}
}

// An instance whose variables no longer resolve the key cannot be repaired
// automatically. The backfill must report it and leave the row alone rather than
// rewriting it to an empty key — an empty correlation key matches every
// subscription for that message name, which would deliver one instance's message
// to all of them.
func TestCorrelationBackfillLeavesUnresolvableKeysAlone(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Backfill Unresolvable Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "order-payment-unresolvable",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "await-payment", Type: entities.IntermediateCatchEvent, Properties: map[string]any{
				"message_name":    "PaymentReceived",
				"correlation_key": "${orderId}",
			}},
			{ID: "confirm", Type: entities.UserTask, Name: "Confirm Order"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "await-payment"},
			{ID: "f2", SourceRef: "await-payment", TargetRef: "confirm"},
			{ID: "f3", SourceRef: "confirm", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "order-payment-unresolvable", map[string]any{"orderId": "order-9"})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	subs, err := h.repo.Subscription().ListByInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	// A template naming a variable this instance never had.
	if err := h.repo.Subscription().UpdateCorrelationKey(ctx, uuid.UUID(subs[0].ID), "${neverSet}"); err != nil {
		t.Fatalf("stage the unresolvable correlation key: %v", err)
	}

	result, err := service_impl2.BackfillMessageCorrelationKeys(ctx, h.repo)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if result.Unresolved != 1 || result.Rewritten != 0 {
		t.Errorf("expected 1 unresolved / 0 rewritten, got %+v", result)
	}

	after, err := h.repo.Subscription().ListByInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("re-list subscriptions: %v", err)
	}
	if after[0].CorrelationKey != "${neverSet}" {
		t.Errorf("the unresolvable key was modified to %q; it must be left intact for manual repair", after[0].CorrelationKey)
	}
}

// Guard the reason the unresolvable row is left alone rather than blanked: an
// empty correlation key is treated as "do not filter", so a blanked row would
// receive every message sent for that name.
func TestEmptyCorrelationKeyMatchesEverySubscription(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Empty Key Project")

	subs, err := h.repo.Subscription().FindMessages(ctx, h.projID, "PaymentReceived", "")
	if err != nil {
		t.Fatalf("find messages: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("expected no subscriptions in a fresh project, got %d", len(subs))
	}

	// Two instances of different conversations.
	for _, key := range []string{"order-a", "order-b"} {
		if err := h.repo.Subscription().Create(ctx, subscriptionRow(h.projID, "PaymentReceived", key)); err != nil {
			t.Fatalf("create subscription %s: %v", key, err)
		}
	}

	matched, err := h.repo.Subscription().FindMessages(ctx, h.projID, "PaymentReceived", "")
	if err != nil {
		t.Fatalf("find messages with an empty key: %v", err)
	}
	if len(matched) != 2 {
		t.Fatalf("expected an empty correlation key to match both subscriptions, got %d", len(matched))
	}

	targeted, err := h.repo.Subscription().FindMessages(ctx, h.projID, "PaymentReceived", "order-a")
	if err != nil {
		t.Fatalf("find messages with a real key: %v", err)
	}
	if len(targeted) != 1 {
		t.Errorf("expected a real correlation key to match exactly one subscription, got %d", len(targeted))
	}
}

func subscriptionRow(projectID uuid.UUID, messageName, correlationKey string) models.Subscription {
	id, _ := uuid.NewV7()
	return models.Subscription{
		Base:           models.Base{ID: models.UUID(id)},
		ProjectID:      models.UUID(projectID),
		InstanceID:     models.UUID(uuid.New()),
		NodeID:         "await-payment",
		Type:           models.SubscriptionMessage,
		EventName:      messageName,
		CorrelationKey: correlationKey,
	}
}
