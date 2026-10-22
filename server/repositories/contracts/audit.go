package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

type AuditRepository interface {
	Create(ctx context.Context, entry models.AuditModel) error
	ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.AuditModel, error)
	// LatestByInstance returns limit entries of an instance's history counted
	// back from its newest, after skipping the newest offset, oldest first; and
	// how many entries the instance has in all.
	LatestByInstance(ctx context.Context, instanceID uuid.UUID, limit, offset int64) ([]models.AuditModel, int64, error)
	// NodeVisits counts the entries of eventType for each node of an
	// instance, in the order each node was first reached.
	NodeVisits(ctx context.Context, instanceID uuid.UUID, eventType string) ([]NodeVisit, error)
	// FirstByInstance returns an instance's oldest entry of eventType, and
	// false when it has none.
	FirstByInstance(ctx context.Context, instanceID uuid.UUID, eventType string) (models.AuditModel, bool, error)
	// ListByProject returns at most limit of a project's entries, oldest
	// first. The limit is the caller's, because a project's whole trail is
	// more than any one read should hold.
	ListByProject(ctx context.Context, projectID uuid.UUID, limit int64) ([]models.AuditModel, error)
	// ListOperations returns the entries that name who acted, newest first,
	// within the caller's scope: the audit log. Entries the engine wrote on its
	// own name nobody and are never in it.
	ListOperations(ctx context.Context, filter OperationFilter) ([]models.AuditModel, error)
}

// OperationFilter narrows the audit log. Every field is optional but Limit.
type OperationFilter struct {
	// Actor is who acted, exactly as the entry names them.
	Actor string
	// Type is one kind of entry, such as node_skipped.
	Type      string
	ProjectID uuid.UUID
	// From is the earliest moment an entry may be from, and To the moment
	// every entry is before.
	From, To time.Time
	// Before continues a page: only entries older than this one.
	Before *OperationCursor
	Limit  int64
}

// OperationCursor is a place in the audit log: the entry a page ended on.
// Every entry in the log has a seq, because the log holds only entries
// written after migration 46, and migration 28 numbered every entry since.
type OperationCursor struct {
	At  time.Time
	Seq int64
}

// NodeVisit is how often an instance reached one node.
type NodeVisit struct {
	NodeID string
	Visits int
}
