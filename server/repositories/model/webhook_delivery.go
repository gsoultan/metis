package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// WebhookDelivery is one delivery already acted on.
//
// The pair (webhook, delivery id) is unique, which is the whole point: a sender
// retrying a delivery it is unsure about must not start a second process.
type WebhookDelivery struct {
	storm.Model

	Webhook Webhook

	// DeliveryID is the sender's own identifier for this delivery.
	DeliveryID string
	ReceivedAt time.Time

	DeletedAt *time.Time
}

func (d *WebhookDelivery) Schema(t *storm.Table) {
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&d.DeliveryID).FillNulls(blank)
	t.Col(&d.DeliveryID).Size(191)
	// Across the deleted rows: a delivery id is how a redelivery is recognised
	// as the same one. A marked row that stopped conflicting would let the same
	// event be delivered twice.
	t.UniqueAcrossDeleted(&d.Webhook, &d.DeliveryID)
	t.Col(&d.ReceivedAt).Index()
	t.Col(&d.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&d.DeletedAt)
	// Old deliveries are purged a batch at a time (HardDeleteBatch).
	t.Takes()
}
