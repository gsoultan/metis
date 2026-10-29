package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// OutgoingWebhookDelivery is one event owed to one outgoing webhook: the queue
// and, once it is settled, the log.
//
// A row is written in the transaction of the step that raised the event, so it
// exists exactly when the step committed. The worker claims a due row by moving
// its next_attempt_at past a lease, posts it outside any transaction, and
// records what came of it. A worker that dies mid-post leaves the row due again
// when the lease runs out.
type OutgoingWebhookDelivery struct {
	storm.Model

	Webhook OutgoingWebhook

	Event string
	// Body is the exact JSON posted, sealed by the repository: it names the
	// instance and its business key, which are the project's data.
	Body string

	Status        string
	Attempts      int
	NextAttemptAt time.Time

	LastAttemptAt  *time.Time
	LastStatusCode *int
	LastError      *string
	// LastResponse is the start of what the receiver answered last, which is
	// usually where it says why it refused.
	LastResponse *string

	// Test marks a delivery somebody asked for by hand, to see whether the
	// receiver answers. It is logged like any other and never retried.
	Test bool
}

func (d *OutgoingWebhookDelivery) Schema(t *storm.Table) {
	t.Col(&d.Event).Size(64)
	t.Col(&d.Status).Size(16)
	t.Col(&d.LastError).Size(1024)
	t.Col(&d.LastResponse).Size(1024)
	// The claim: pending rows whose time has come, soonest first. Partial, so
	// the index stays the size of the queue rather than of the log. Spelled
	// as PostgreSQL prints it back, as OutgoingWebhook's is.
	t.Index(&d.NextAttemptAt).Where("((status)::text = 'pending'::text)").Named("ix_outgoing_webhook_deliveries_due")
	// A webhook's log, newest first.
	t.Index(&d.Webhook, &d.CreatedAt, &d.ID).Named("ix_outgoing_webhook_deliveries_webhook")
	// The retention sweep.
	t.Index(&d.CreatedAt).Named("ix_outgoing_webhook_deliveries_created")
	// Which removes a batch at a time (DeleteBatch); the claim takes the due
	// rows of switched-on webhooks in one statement (Take over the probe).
	t.Takes()
	t.Probes(&d.Webhook)
}
