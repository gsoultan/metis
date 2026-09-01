package models

import "time"

// BroadcastEventModel is one SSE payload, written so that replicas other than
// the one that produced it can deliver it to their own connected browsers.
//
// The SSE client registry is necessarily per-process — it holds open HTTP
// response writers — so without this table a browser connected to replica A
// never learns about anything that happened on replica B, and its lists
// silently stop updating. This is the shared bus that closes that gap.
type BroadcastEventModel struct {
	// A database-assigned sequence rather than a UUID: readers poll for "what
	// is newer than what I have already delivered", which needs an ordering the
	// database agrees on.
	ID int64 `gorm:"primaryKey;autoIncrement"`

	// Origin is the replica that produced the event. A replica delivers its own
	// events to its own clients immediately, so it skips its own rows here
	// rather than delivering them twice.
	Origin string `gorm:"size:64;not null;index:ix_broadcast_events_origin_id,priority:2"`

	// Payload is the already-encoded SSE `data:` body. Encoding once at the
	// producer keeps this table indifferent to what an event contains.
	Payload string `gorm:"type:text;not null"`

	// CreatedAt is only for pruning. Delivery ordering is ID.
	CreatedAt time.Time `gorm:"not null;index:ix_broadcast_events_created_at"`
}

func (BroadcastEventModel) TableName() string {
	return "broadcast_events"
}
