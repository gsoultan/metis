package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// IdempotencyRecord is what a caller's Idempotency-Key already produced.
//
// In the database rather than in the process, so a retry that lands on another
// replica gets the original answer instead of executing the write a second
// time.
//
// The key is the primary key — there is no surrogate id — and the column is
// record_key rather than key, which is reserved on several engines.
type IdempotencyRecord struct {
	Key string
	// RequestHash is what the original request looked like. A repeat of the key
	// with a different body is a different request wearing the same name, and
	// is refused rather than answered with the first one's result.
	RequestHash string
	Completed   bool

	StatusCode int
	Headers    storm.JSON
	Body       []byte

	CreatedAt   time.Time
	CompletedAt *time.Time
}

func (i *IdempotencyRecord) Schema(t *storm.Table) {
	// Renamed before the key is declared; see SharedCounter for why the order
	// is not cosmetic.
	t.Col(&i.Key).Named("record_key").Size(64)
	t.PrimaryKey(&i.Key)
	t.Col(&i.RequestHash).Size(64)
	t.Col(&i.Completed).Index()
	t.Col(&i.CreatedAt).Index()
	// The sweep removes expired records a batch at a time (DeleteBatch).
	t.Takes()
}
