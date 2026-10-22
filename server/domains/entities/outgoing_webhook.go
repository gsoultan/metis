package entities

import (
	"slices"
	"time"

	"github.com/google/uuid"
)

// OutgoingWebhook is an address this installation tells about what happens in
// a project's processes: a ledger that wants every completed purchase, a pager
// that wants every incident.
//
// It is the outbound half of an integration, the counterpart of Webhook. What
// it receives is delivered from a queue that commits with the step that caused
// it (OutgoingWebhookDelivery), so a step that rolled back is never announced
// and one that committed is announced until the receiver accepts it or the
// retries run out.
//
// No JSON or ORM tags: an endpoint maps it to a view, and a repository maps it
// to a row.
type OutgoingWebhook struct {
	ID        uuid.UUID
	CreatedAt time.Time
	Project   *Project
	Name      string

	// URL is where deliveries are posted. It is untrusted input like any other
	// destination an author names, so it is checked against the egress policy
	// when it is saved and dialled through the guarded client when it is used.
	URL string

	// Events are the kinds of event it is sent, as OutgoingWebhookEvent names.
	Events []OutgoingWebhookEvent

	// Secret signs every delivery. It is returned exactly once, when the
	// webhook is created; it is encrypted at rest and no read gives it back.
	Secret string

	Enabled bool
}

// OutgoingWebhookEvent names a kind of event a receiver can ask for.
//
// These are the names a receiver sees, in its configuration and in every
// delivery, so they are a public contract: dotted and lower case, the way
// receivers' own event names usually are, and never renamed once shipped.
type OutgoingWebhookEvent string

const (
	OutgoingProcessStarted   OutgoingWebhookEvent = "process.started"
	OutgoingProcessCompleted OutgoingWebhookEvent = "process.completed"
	OutgoingTaskCreated      OutgoingWebhookEvent = "task.created"
	OutgoingTaskCompleted    OutgoingWebhookEvent = "task.completed"
	OutgoingIncidentRaised   OutgoingWebhookEvent = "incident.raised"
)

// outgoingEventOf maps the engine's event types to the names a receiver asks
// for. An engine event that is not here is never sent anywhere: NodeReached
// fires on every step and is the engine's business, not a receiver's.
var outgoingEventOf = map[string]OutgoingWebhookEvent{
	EventProcessStarted:   OutgoingProcessStarted,
	EventProcessCompleted: OutgoingProcessCompleted,
	EventTaskCreated:      OutgoingTaskCreated,
	EventTaskCompleted:    OutgoingTaskCompleted,
	EventIncidentRaised:   OutgoingIncidentRaised,
}

// OutgoingEventFor returns the name a receiver knows an engine event by, and
// false for an engine event no receiver can ask for.
func OutgoingEventFor(engineType string) (OutgoingWebhookEvent, bool) {
	event, ok := outgoingEventOf[engineType]
	return event, ok
}

// OutgoingWebhookEvents is every event a receiver can ask for, in the order a
// form lists them.
func OutgoingWebhookEvents() []OutgoingWebhookEvent {
	return []OutgoingWebhookEvent{
		OutgoingProcessStarted, OutgoingProcessCompleted,
		OutgoingTaskCreated, OutgoingTaskCompleted,
		OutgoingIncidentRaised,
	}
}

// Known reports whether a receiver can ask for this event.
func (e OutgoingWebhookEvent) Known() bool {
	return slices.Contains(OutgoingWebhookEvents(), e)
}
