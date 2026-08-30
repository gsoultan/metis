package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// AuditWriter is the service-level contract for persisting Business Timeline
// narrative events. Callers supply a fully constructed AuditEntry; the writer
// is responsible for generating the human-readable Narrative and persisting it.
type AuditWriter interface {
	// RecordEvent persists a narrative audit entry for the given lifecycle event.
	RecordEvent(ctx context.Context, entry entities.AuditEntry) error
}
