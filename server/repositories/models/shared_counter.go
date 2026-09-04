package models

import "time"

// SharedCounterModel is one replica's contribution to a count that has to be
// enforced across all of them.
//
// Rate limits and circuit breakers were per-process, so N replicas applied each
// limit N times over: a partner's quota exceeded N-fold, a limit that admitted
// N times the configured traffic, and N breakers deciding independently whether
// a downstream was healthy.
//
// The row is keyed by replica as well as by counter, and that is the whole
// design. **Each replica writes only its own row**, so a flush is an ordinary
// insert-or-update with no contention, no conditional upsert, and no dialect
// disagreement about how to express one — the thing that made the idempotency
// store hard. The shared value is a SUM across replicas at read time.
type SharedCounterModel struct {
	// Scope separates counters that share a key space — an HTTP client address
	// and a connector target could otherwise collide.
	Scope string `gorm:"size:64;not null;primaryKey"`

	// Key is what is being counted: a client address, a connector target.
	//
	// The column is counter_key, not key. `key` is reserved in MySQL, so a
	// SELECT naming it unquoted is a syntax error there and nowhere else — the
	// same trap the idempotency store hit, which is why its column is
	// record_key. Sized rather than unbounded text because it is part of the
	// primary key, and MySQL cannot index an unbounded column.
	Key string `gorm:"column:counter_key;size:255;not null;primaryKey"`

	// Replica is which process contributed this count. Part of the key so that
	// two replicas never write the same row.
	Replica string `gorm:"size:64;not null;primaryKey"`

	// WindowStart is the start of the window this count belongs to, truncated
	// to the window length. Part of the key so an expiring window is a
	// different row rather than an update racing a reset.
	WindowStart time.Time `gorm:"not null;primaryKey"`

	// Count is this replica's own total for the window. Written absolutely
	// rather than incrementally: the replica owns the row, so it knows the
	// value, and an absolute write cannot lose an increment to a lost update.
	Count int64 `gorm:"not null"`

	UpdatedAt time.Time `gorm:"not null;index:ix_shared_counters_updated_at"`
}

func (SharedCounterModel) TableName() string {
	return "shared_counters"
}
