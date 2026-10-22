package impl

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

type queuedEvent struct {
	project uuid.UUID
	event   entities.OutgoingWebhookEvent
	body    []byte
}

type recordingQueue struct {
	queued []queuedEvent
	err    error
}

func (q *recordingQueue) Enqueue(_ context.Context, projectID uuid.UUID, event entities.OutgoingWebhookEvent, body []byte) (int, error) {
	if q.err != nil {
		return 0, q.err
	}
	q.queued = append(q.queued, queuedEvent{project: projectID, event: event, body: body})
	return 1, nil
}

func (q *recordingQueue) ClaimDue(context.Context, time.Time, time.Duration, int) ([]entities.OutgoingWebhookDelivery, error) {
	return nil, nil
}

func (q *recordingQueue) Settle(context.Context, entities.OutgoingWebhookDelivery, entities.OutgoingWebhookAttempt) error {
	return nil
}

func (q *recordingQueue) ForgetBefore(context.Context, time.Time) (int64, error) { return 0, nil }

func directly(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }

// Only the events a receiver can ask for are queued, under the names it asks
// for them by, for the project they happened in.
func TestOnlyTheEventsAReceiverCanAskForAreQueued(t *testing.T) {
	queue := &recordingQueue{}
	observer := NewOutgoingWebhookObserver(queue, directly)
	project := uuid.New()
	instance := &entities.ProcessInstance{ID: uuid.New(), Project: &entities.Project{ID: project}}

	for _, engineType := range []string{
		entities.EventNodeReached, entities.EventTaskClaimed, entities.EventTaskUpdated,
		entities.EventProcessStarted, entities.EventTaskCreated, entities.EventTaskCompleted, entities.EventProcessCompleted,
	} {
		observer.OnEvent(t.Context(), entities.ProcessEvent{Type: engineType, Instance: instance, Timestamp: time.Now().Unix()})
	}

	want := []entities.OutgoingWebhookEvent{
		entities.OutgoingProcessStarted, entities.OutgoingTaskCreated,
		entities.OutgoingTaskCompleted, entities.OutgoingProcessCompleted,
	}
	if len(queue.queued) != len(want) {
		t.Fatalf("queued %d events, want %d: %+v", len(queue.queued), len(want), queue.queued)
	}
	for i, got := range queue.queued {
		if got.event != want[i] || got.project != project {
			t.Errorf("queued[%d] = %s for %s, want %s for %s", i, got.event, got.project, want[i], project)
		}
	}
}

// A delivery goes to an address somebody typed, so it says what happened and
// to which case and carries none of the instance's business data.
func TestADeliveryCarriesNoneOfTheInstancesVariables(t *testing.T) {
	queue := &recordingQueue{}
	project := uuid.New()
	taskID := uuid.New()
	NewOutgoingWebhookObserver(queue, directly).OnEvent(t.Context(), entities.ProcessEvent{
		Type: entities.EventTaskCompleted,
		Instance: &entities.ProcessInstance{
			ID: uuid.New(), BusinessKey: "PO-4182", Status: entities.ProcessActive,
			Definition: &entities.ProcessDefinition{Key: "purchase", Name: "Purchase approval", Version: 3},
			Variables:  map[string]any{"salary": 98765, "iban": "DE89370400440532013000"},
		},
		Project:   &entities.Project{ID: project},
		Node:      &entities.Node{ID: "approve", Name: "Manager approval"},
		TaskID:    taskID,
		Assignee:  "alice",
		Timestamp: time.Date(2026, 10, 21, 10, 41, 0, 0, time.UTC).Unix(),
		Variables: map[string]any{"decision": "approved", "comment": "fine"},
	})
	if len(queue.queued) != 1 {
		t.Fatalf("queued %d events, want 1", len(queue.queued))
	}
	body := string(queue.queued[0].body)
	for _, secret := range []string{"98765", "DE89370400440532013000", "salary", "approved", "comment"} {
		if strings.Contains(body, secret) {
			t.Errorf("the delivery carries %q from the instance's data: %s", secret, body)
		}
	}

	var payload entities.OutgoingWebhookPayload
	if err := json.Unmarshal(queue.queued[0].body, &payload); err != nil {
		t.Fatalf("the body is not the payload: %v", err)
	}
	if payload.Event != entities.OutgoingTaskCompleted || payload.ProjectID != project || payload.EventID == uuid.Nil {
		t.Errorf("payload = %+v, want a task.completed event of the project with its own id", payload)
	}
	if payload.Instance == nil || payload.Instance.BusinessKey != "PO-4182" || payload.Instance.ProcessKey != "purchase" || payload.Instance.Version != 3 {
		t.Errorf("instance = %+v, want the case named by its business key and process", payload.Instance)
	}
	if payload.Task == nil || payload.Task.ID != taskID || payload.Task.Assignee != "alice" {
		t.Errorf("task = %+v, want the task and who holds it", payload.Task)
	}
	if payload.Node == nil || payload.Node.Name != "Manager approval" {
		t.Errorf("node = %+v, want the step", payload.Node)
	}
	if !payload.OccurredAt.Equal(time.Date(2026, 10, 21, 10, 41, 0, 0, time.UTC)) {
		t.Errorf("occurred_at = %s, want when the event happened", payload.OccurredAt)
	}
}

// A queue that cannot be written must not fail, or panic in, the step that
// raised the event.
func TestAQueueThatCannotBeWrittenDoesNotFailTheStep(t *testing.T) {
	queue := &recordingQueue{err: errors.New("the database went away")}
	NewOutgoingWebhookObserver(queue, directly).OnEvent(t.Context(), entities.ProcessEvent{
		Type:    entities.EventProcessStarted,
		Project: &entities.Project{ID: uuid.New()},
	})
}

func TestAnEventOfNoProjectIsNotQueued(t *testing.T) {
	queue := &recordingQueue{}
	NewOutgoingWebhookObserver(queue, directly).OnEvent(t.Context(), entities.ProcessEvent{Type: entities.EventProcessStarted})
	if len(queue.queued) != 0 {
		t.Errorf("queued %d events of no project", len(queue.queued))
	}
}
