package models

import "time"

// IdempotencyRecordModel remembers what a caller's Idempotency-Key already
// produced, so a retry gets the original answer instead of doing the work
// twice.
//
// It exists in the database rather than in the serving process because the
// in-memory version only protected one replica: a client retry that landed on
// a different one found an empty cache and executed the write again. For a
// process engine that is a duplicate business action — the exact thing the
// header exists to prevent.
type IdempotencyRecordModel struct {
	// Key is the SHA-256 of the scoped storage key: method, path, tenant, user
	// and the client's header value. It is hashed because those parts have no
	// length bound — a path plus a client-chosen key can be arbitrarily long —
	// and a primary key needs one. 64 hex characters is fixed and well inside
	// every dialect's index limit.
	//
	// The column is `record_key`, not `key`: **`key` is reserved in MySQL**, so
	// the table avoids the word rather than quoting it at every call site.
	Key string `json:"key"`

	// RequestHash detects a key reused for a different request, which is a
	// client bug worth refusing rather than serving the wrong cached answer to.
	RequestHash string `json:"request_hash"`

	// Completed separates "somebody is running this right now" from "here is
	// the answer". A claimed-but-incomplete row is what makes a second caller
	// wait rather than execute.
	Completed bool `json:"completed"`

	StatusCode int                 `json:"status_code,omitzero"`
	Headers    map[string][]string `json:"headers,omitzero"`
	Body       []byte              `json:"body,omitzero"`

	// CreatedAt is what the retention sweep works from. These rows answer "have
	// I already done this?" for as long as a client might retry, and are
	// worthless after that.
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitzero"`
}

// TableName overrides the table name for IdempotencyRecordModel.
func (IdempotencyRecordModel) TableName() string {
	return "idempotency_records"
}
