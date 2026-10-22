package entities

import (
	"time"

	"github.com/google/uuid"
)

// Bounds of one page of the audit log.
const (
	// OperationPageDefault is how many entries a page holds when the caller
	// does not say.
	OperationPageDefault = 50
	// OperationPageMax is the most a page holds, whatever the caller asks.
	OperationPageMax = 200
)

// OperationQuery narrows the audit log: what people did, newest first. Every
// field is optional.
type OperationQuery struct {
	// Actor is who acted, exactly as the entries name them.
	Actor string
	// Type is one kind of entry, such as node_skipped.
	Type      string
	ProjectID uuid.UUID
	// From is the earliest moment an entry may be from; To is the moment
	// every entry is before.
	From, To time.Time
	// Before is the Next of the page before this one.
	Before string
	Limit  int
}

// OperationPage is one page of the audit log, newest first. Next continues it
// with the entries older than the last; it is empty on the last page.
type OperationPage struct {
	Operations []AuditEntry
	Next       string
}
