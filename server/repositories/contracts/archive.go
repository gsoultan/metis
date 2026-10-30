package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

// ArchiveFilter narrows a project's archive. Every field is optional; the
// cursor pages on, from the last row of the page before.
type ArchiveFilter struct {
	ProcessKey  string
	BusinessKey string
	// FinishedBefore and AfterID are the last row of the page before: the
	// next page starts after it, in the list's order (most recently finished
	// first, then by id).
	FinishedBefore time.Time
	AfterID        uuid.UUID
	Limit          int64
}

// ArchiveRepository reads the archive the retention sweep writes, and
// removes records past the archive's own period. Reads are scoped to the
// caller's organization: somebody else's archived instance is answered as one
// that is not there.
type ArchiveRepository interface {
	// List reads a project's archived instances, most recently finished
	// first.
	List(ctx context.Context, projectID uuid.UUID, filter ArchiveFilter) ([]models.ArchivedInstanceModel, error)
	// Get reads one archived instance and opens its record.
	Get(ctx context.Context, instanceID uuid.UUID) (models.ArchivedInstanceModel, models.ArchiveRecord, error)
	// MoveRecordsOut moves the records still kept in their rows to the store
	// the operator chose for them; with none chosen it does nothing. System
	// context only.
	MoveRecordsOut(ctx context.Context) (int64, error)
	// ForgetArchivedBefore removes the records archived before cutoff, from
	// wherever they are kept. System context only: it spans every tenant.
	ForgetArchivedBefore(ctx context.Context, cutoff time.Time) (int64, error)
}
