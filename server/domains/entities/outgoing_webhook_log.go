package entities

import (
	"time"

	"github.com/google/uuid"
)

// OutgoingWebhookLogEntry is one delivery as its webhook's log shows it: what
// was sent, where it stands, and what came of its last attempt.
type OutgoingWebhookLogEntry struct {
	ID        uuid.UUID
	WebhookID uuid.UUID
	Event     OutgoingWebhookEvent
	Status    OutgoingWebhookDeliveryStatus

	// Attempts is how many have been made; NextAttemptAt is when the next one
	// is due, meaningful only while the delivery is pending.
	Attempts      int
	NextAttemptAt time.Time

	// LastAttemptAt is nil until the first attempt; LastStatusCode is zero
	// when the last attempt had no answer.
	LastAttemptAt  *time.Time
	LastStatusCode int
	LastError      string
	LastResponse   string

	// Test marks a delivery somebody sent from the webhook's page, which the
	// worker never retries.
	Test bool

	// Body is the exact bytes posted. It carries no process variables.
	Body      []byte
	CreatedAt time.Time
}

// OutgoingWebhookLogFilter narrows and pages a webhook's log, newest first.
type OutgoingWebhookLogFilter struct {
	// Status, when set, keeps only the deliveries standing there.
	Status OutgoingWebhookDeliveryStatus

	// Before, when set, starts the page after the delivery it names: a log is
	// paged by its own order, so a page is never shifted by what arrives
	// while somebody reads it.
	Before *OutgoingWebhookLogCursor

	Limit int
}

// OutgoingWebhookLogCursor is where a page of the log ended.
type OutgoingWebhookLogCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// OutgoingWebhookTest is what came of sending a webhook a sample event: the
// request exactly as it was posted, signature included, and the receiver's
// answer.
type OutgoingWebhookTest struct {
	DeliveryID uuid.UUID
	Event      OutgoingWebhookEvent

	URL     string
	Headers map[string]string
	Body    []byte

	Attempt  OutgoingWebhookAttempt
	Duration time.Duration
}
