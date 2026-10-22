package entities

import (
	"time"

	"github.com/google/uuid"
)

// OutgoingWebhookPayload is the body of every outgoing webhook delivery: a
// public contract, so fields are added and never renamed or removed.
//
// It names what happened and to which case, and carries none of the process's
// variables. Those are the project's business data — amounts, names, the
// contents of a form — and a webhook is an address anybody with the right
// role can type. A receiver that needs more asks the API for it, with
// credentials of its own, which is a decision somebody makes on purpose.
type OutgoingWebhookPayload struct {
	// EventID is the event's own id, the same in every webhook it is sent to.
	// The delivery id in X-Delivery-Id is per webhook.
	EventID    uuid.UUID            `json:"event_id"`
	Event      OutgoingWebhookEvent `json:"event"`
	OccurredAt time.Time            `json:"occurred_at"`
	ProjectID  uuid.UUID            `json:"project_id"`

	Instance *OutgoingWebhookInstance `json:"instance,omitzero"`
	Node     *OutgoingWebhookNode     `json:"node,omitzero"`
	Task     *OutgoingWebhookTask     `json:"task,omitzero"`

	// Test is true on a delivery somebody sent by hand to try the receiver.
	Test bool `json:"test,omitzero"`
}

// OutgoingWebhookInstance is the case an event is about.
type OutgoingWebhookInstance struct {
	ID          uuid.UUID `json:"id"`
	BusinessKey string    `json:"business_key,omitzero"`
	ProcessKey  string    `json:"process_key,omitzero"`
	ProcessName string    `json:"process_name,omitzero"`
	Version     int       `json:"process_version,omitzero"`
	Status      string    `json:"status,omitzero"`
}

// OutgoingWebhookNode is the step an event happened at.
type OutgoingWebhookNode struct {
	ID   string `json:"id,omitzero"`
	Name string `json:"name,omitzero"`
}

// OutgoingWebhookTask is the task a task event is about.
type OutgoingWebhookTask struct {
	ID       uuid.UUID `json:"id,omitzero"`
	Assignee string    `json:"assignee,omitzero"`
}

// NewOutgoingWebhookPayload describes an engine event to a receiver.
func NewOutgoingWebhookPayload(eventID uuid.UUID, outgoing OutgoingWebhookEvent, projectID uuid.UUID, event ProcessEvent) OutgoingWebhookPayload {
	payload := OutgoingWebhookPayload{
		EventID:    eventID,
		Event:      outgoing,
		OccurredAt: time.Unix(event.Timestamp, 0).UTC(),
		ProjectID:  projectID,
		Instance:   outgoingInstanceOf(event.Instance),
	}
	if event.Node != nil && (event.Node.ID != "" || event.Node.Name != "") {
		payload.Node = &OutgoingWebhookNode{ID: event.Node.ID, Name: event.Node.Name}
	}
	if event.TaskID != uuid.Nil || event.Assignee != "" {
		payload.Task = &OutgoingWebhookTask{ID: event.TaskID, Assignee: event.Assignee}
	}
	return payload
}

func outgoingInstanceOf(instance *ProcessInstance) *OutgoingWebhookInstance {
	if instance == nil {
		return nil
	}
	out := &OutgoingWebhookInstance{
		ID:          instance.ID,
		BusinessKey: instance.BusinessKey,
		Status:      string(instance.Status),
	}
	if definition := instance.Definition; definition != nil {
		out.ProcessKey = definition.Key
		out.ProcessName = definition.Name
		out.Version = definition.Version
	}
	return out
}
