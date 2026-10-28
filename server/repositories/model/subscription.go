package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// Subscription is an instance waiting on an event — a message or a signal.
//
// CorrelationKey is resolved per instance when the subscription is created, not
// stored as the template it was authored as: a subscription still holding a raw
// ${...} matches no inbound value, and the instance waits forever.
type Subscription struct {
	storm.Model

	Project  Project
	Instance ProcessInstance

	NodeID string
	Type   SubscriptionType
	// EventName is the message or signal name being waited on.
	EventName string
	// CorrelationKey narrows it to one instance. Empty means match-all, which
	// is deliberate and is why a message matching nothing still succeeds.
	CorrelationKey *string

	DeletedAt *time.Time
}

func (s *Subscription) Schema(t *storm.Table) {
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&s.NodeID).FillNulls(blank)
	t.Col(&s.Type).FillNulls(blank)
	t.Col(&s.EventName).FillNulls(blank)
	// The table predates the type's name; keeping it avoids renaming a table
	// for cosmetics.
	t.Name("event_subscriptions")
	// No size: a step's id is the definition's, and nothing at deploy limits how long it is.
	t.Col(&s.Type).Size(32)
	t.Col(&s.Type).Index()
	t.Col(&s.EventName).Size(255)
	t.Col(&s.EventName).Index()
	t.Col(&s.CorrelationKey).Size(512)
	t.Col(&s.CorrelationKey).Index()
	t.Col(&s.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&s.DeletedAt)
}
