package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// TaskLinkRepository keeps the one-use links people act on tasks through.
//
// A link is kept by the SHA-256 of its token and found by nothing else. The
// rows are in the database the task is, whichever environment that is.
type TaskLinkRepository interface {
	// Create records a link.
	Create(ctx context.Context, link entities.TaskLink, tokenHash string) error

	// GetByHash returns the link a token hashes to, used, voided and expired
	// ones too: whether it is still good is the caller's to decide.
	// apierr.ErrNotFound when no link has that hash.
	GetByHash(ctx context.Context, tokenHash string) (entities.TaskLink, error)

	// GetByHashForUpdate is GetByHash holding the link's row until the
	// transaction ends, so two submits of one link take turns.
	GetByHashForUpdate(ctx context.Context, tokenHash string) (entities.TaskLink, error)

	// MarkUsed records that a link was used at the given moment.
	MarkUsed(ctx context.Context, id uuid.UUID, at time.Time) error

	// VoidOpenForTask voids every link to a task that is neither used nor
	// voided yet, and reports how many it voided.
	VoidOpenForTask(ctx context.Context, taskID uuid.UUID, at time.Time) (int64, error)
}
