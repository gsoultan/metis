package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// Webhook is a public address a partner posts to, and what to do with what
// arrives.
type Webhook struct {
	storm.Model

	Project Project

	Name string
	// Token is the address's secret component: it is what makes the URL
	// unguessable, so it is unique and never logged.
	Token string
	// Secret signs the payload. Held to verify the signature the sender
	// computed, and never returned by the API.
	Secret          string
	SignatureHeader string

	// MessageName is the process message this delivery correlates to, and
	// CorrelationExpression picks which instance out of the payload.
	MessageName           string
	CorrelationExpression *string

	Enabled bool

	// LegacySignaturesUntil is when the webhook stops accepting signatures
	// over the body alone. Null means it never accepts them.
	LegacySignaturesUntil *time.Time

	DeletedAt *time.Time
}

func (w *Webhook) Schema(t *storm.Table) {
	t.Col(&w.Name).Size(255)
	t.Col(&w.Token).Size(64)
	// Across the deleted rows: the token is a credential. Freeing a deleted
	// webhook's token for reuse means a caller still holding it would one day
	// find it authenticating against somebody else's endpoint.
	t.UniqueAcrossDeleted(&w.Token)
	t.Col(&w.Secret).Size(512)
	t.Col(&w.SignatureHeader).Size(128)
	t.Col(&w.MessageName).Size(255)
	t.Col(&w.CorrelationExpression).Size(512)
	t.Col(&w.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&w.DeletedAt)
}
