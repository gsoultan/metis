package impl

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// What an administrator can do from a webhook's page: send it a test, read its
// log, and move one delivery on or stop it.

// stoppedByHand is what the log says of a delivery somebody stopped.
const stoppedByHand = "stopped by an administrator before it was accepted; nothing more is sent of it"

// SendOutgoingWebhookTest posts a sample event, signed exactly as a real
// delivery is, and waits for the answer.
//
// The sample names no real instance: it is built here, marked "test": true,
// and carries a made-up business key, so a receiver that acts on it acts on
// nothing. It is sent whether or not the webhook is switched on, because
// trying a receiver before switching it on is the point.
func (s *outgoingWebhookService) SendOutgoingWebhookTest(ctx context.Context, id uuid.UUID, event entities.OutgoingWebhookEvent) (entities.OutgoingWebhookTest, error) {
	hook, err := s.webhooks.Signing(ctx, id)
	if err != nil {
		return entities.OutgoingWebhookTest{}, err
	}
	event, err = testEventFor(hook, event)
	if err != nil {
		return entities.OutgoingWebhookTest{}, err
	}
	delivery, err := sampleDelivery(hook, event, s.now())
	if err != nil {
		return entities.OutgoingWebhookTest{}, err
	}

	started := s.now()
	test := entities.OutgoingWebhookTest{
		DeliveryID: delivery.ID,
		Event:      event,
		URL:        hook.URL,
		Body:       delivery.Body,
	}
	req, err := signedRequest(ctx, delivery, started)
	if err != nil {
		test.Attempt = unmadeAttempt(err, started)
	} else {
		test.Headers = make(map[string]string, len(req.Header))
		for name := range maps.Keys(req.Header) {
			test.Headers[name] = req.Header.Get(name)
		}
		test.Attempt = s.sender.send(req, started)
	}
	test.Duration = s.now().Sub(started)

	if err := s.webhooks.RecordTest(ctx, hook.ID, test); err != nil {
		return entities.OutgoingWebhookTest{}, err
	}
	return test, nil
}

// testEventFor is the event a test sends: the one asked for, or else the
// webhook's first. Any known event may be tried, including one the webhook is
// not yet sent, so a receiver can be readied before the webhook asks for it.
func testEventFor(hook entities.OutgoingWebhook, asked entities.OutgoingWebhookEvent) (entities.OutgoingWebhookEvent, error) {
	if asked == "" {
		if len(hook.Events) == 0 {
			return "", apierr.Invalidf("this webhook is sent no events, so there is nothing to test it with")
		}
		return hook.Events[0], nil
	}
	if !asked.Known() {
		return "", apierr.Invalidf("%q is not an event a webhook can be sent; choose from %s", asked, knownOutgoingEvents())
	}
	return asked, nil
}

// sampleDelivery renders the test body for event, as a delivery of the
// webhook with an id of its own.
func sampleDelivery(hook entities.OutgoingWebhook, event entities.OutgoingWebhookEvent, now time.Time) (entities.OutgoingWebhookDelivery, error) {
	deliveryID, err := uuid.NewV7()
	if err != nil {
		return entities.OutgoingWebhookDelivery{}, fmt.Errorf("outgoing webhook test: could not generate an id: %w", err)
	}
	eventID, err := uuid.NewV7()
	if err != nil {
		return entities.OutgoingWebhookDelivery{}, fmt.Errorf("outgoing webhook test: could not generate an id: %w", err)
	}
	payload := entities.OutgoingWebhookPayload{
		EventID:    eventID,
		Event:      event,
		OccurredAt: now.Truncate(time.Second),
		Instance: &entities.OutgoingWebhookInstance{
			ID:          uuid.New(),
			BusinessKey: "TEST-0001",
			ProcessKey:  "sample",
			ProcessName: "Sample process",
			Version:     1,
			Status:      string(entities.ProcessActive),
		},
		Test: true,
	}
	if hook.Project != nil {
		payload.ProjectID = hook.Project.ID
	}
	switch event {
	case entities.OutgoingTaskCreated, entities.OutgoingTaskCompleted:
		payload.Node = &entities.OutgoingWebhookNode{ID: "review", Name: "Review"}
		payload.Task = &entities.OutgoingWebhookTask{ID: uuid.New(), Assignee: "test.user"}
	case entities.OutgoingProcessCompleted:
		payload.Instance.Status = string(entities.ProcessCompleted)
	case entities.OutgoingIncidentRaised:
		payload.Node = &entities.OutgoingWebhookNode{ID: "charge", Name: "Charge the card"}
		payload.Incident = &entities.OutgoingWebhookIncident{ID: uuid.New()}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return entities.OutgoingWebhookDelivery{}, err
	}
	return entities.OutgoingWebhookDelivery{
		ID:        deliveryID,
		Webhook:   &hook,
		Event:     event,
		Body:      body,
		Status:    entities.DeliveryPending,
		Attempts:  1,
		CreatedAt: now,
	}, nil
}

func (s *outgoingWebhookService) ListOutgoingWebhookDeliveries(ctx context.Context, id uuid.UUID, filter entities.OutgoingWebhookLogFilter) ([]entities.OutgoingWebhookLogEntry, error) {
	switch filter.Status {
	case "", entities.DeliveryPending, entities.DeliveryDelivered, entities.DeliveryFailed:
	default:
		return nil, apierr.Invalidf("%q is not where a delivery can stand; ask for pending, delivered or failed", filter.Status)
	}
	return s.webhooks.Deliveries(ctx, id, filter)
}

func (s *outgoingWebhookService) RetryOutgoingWebhookDelivery(ctx context.Context, deliveryID uuid.UUID) error {
	return s.webhooks.RetryDelivery(ctx, deliveryID, s.now())
}

func (s *outgoingWebhookService) StopOutgoingWebhookDelivery(ctx context.Context, deliveryID uuid.UUID) error {
	return s.webhooks.StopDelivery(ctx, deliveryID, s.now(), stoppedByHand)
}
