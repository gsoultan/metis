package entities

import (
	"time"

	"github.com/google/uuid"
)

// OutgoingWebhookDeliveryStatus is where a delivery stands.
type OutgoingWebhookDeliveryStatus string

const (
	// DeliveryPending is waiting for its next attempt, which may be its first.
	DeliveryPending OutgoingWebhookDeliveryStatus = "pending"
	// DeliveryDelivered was accepted by the receiver with a 2xx.
	DeliveryDelivered OutgoingWebhookDeliveryStatus = "delivered"
	// DeliveryFailed ran out of attempts, or met an answer no retry changes.
	// It stays in the log so that somebody can see what was lost and why.
	DeliveryFailed OutgoingWebhookDeliveryStatus = "failed"
)

// OutgoingWebhookDelivery is one event owed to one receiver.
//
// Its ID is the idempotency key the receiver sees in X-Delivery-Id. It is the
// same on every attempt, so a receiver that accepted an event and answered too
// late to be believed can recognise the retry and do nothing.
type OutgoingWebhookDelivery struct {
	ID      uuid.UUID
	Webhook *OutgoingWebhook
	Event   OutgoingWebhookEvent

	// Body is the exact bytes posted, rendered once when the event commits so
	// that every attempt sends, and signs, the same thing.
	Body []byte

	Status        OutgoingWebhookDeliveryStatus
	Attempts      int
	NextAttemptAt time.Time
	CreatedAt     time.Time
}

// OutgoingWebhookAttempt is what came of one attempt to deliver.
type OutgoingWebhookAttempt struct {
	// StatusCode is the receiver's answer, zero when there was none.
	StatusCode int
	// Error says why the attempt failed, in words, when it did.
	Error string
	// Response is the start of the receiver's answer, when it gave one.
	Response string
	// Permanent says no retry could change the answer: the address is one
	// this installation refuses to call.
	Permanent bool
	At        time.Time
}

// Accepted reports whether the receiver took the delivery.
func (a OutgoingWebhookAttempt) Accepted() bool {
	return a.Error == "" && a.StatusCode >= 200 && a.StatusCode < 300
}
