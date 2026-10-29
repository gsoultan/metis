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
	ID int64

	// Origin is the replica that produced the event. A replica delivers its own
	// events to its own clients immediately, so it skips its own rows here
	// rather than delivering them twice.
	Origin string

	// Payload is the already-encoded SSE `data:` body. Encoding once at the
	// producer keeps this table indifferent to what an event contains.
	Payload string

	// OrganizationID and EnvironmentID are who the event is for.
	//
	// They travel with the payload because the replica that delivers it has no
	// context to recover them from: the request that produced the event
	// happened on another machine. Without them a peer replica could only
	// deliver to everybody, which is the leak this scoping closes — and closing
	// it on one replica while the bus reopened it would be no fix at all.
	//
	// Nullable so rows written before this column existed still read; they are
	// dropped on delivery rather than broadcast, because an event whose
	// audience is unknown has no safe audience.
	OrganizationID *UUID
	EnvironmentID  *UUID `json:"environment_id,omitzero"`

	// CreatedAt is only for pruning. Delivery ordering is ID.
	CreatedAt time.Time
}

func (BroadcastEventModel) TableName() string {
	return "broadcast_events"
}
