package contracts

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// ErrRetentionHoldOutsideTransaction is what placing or releasing a hold
// answers when no transaction is open. The hold and the trail entry that says
// who placed it are written together or not at all, and the sweep's wait for
// holds being placed (ProcessRepository.ForgetFinishedBefore) is a wait for
// the transaction that places one.
var ErrRetentionHoldOutsideTransaction = errors.New("a retention hold is placed and released inside a transaction, and none is open")

// ErrInstanceAlreadyHeld is what Place answers when the instance is already
// held. The database decides it (the unique index on the instance), so of two
// holds placed at once one is written and the other gets this.
var ErrInstanceAlreadyHeld = errors.New("this instance is already held")

// RetentionHoldRepository stores the holds that keep instances out of the
// finished-instance sweep. Every method is scoped to the caller's
// organization: a hold on somebody else's instance is answered as one that is
// not there.
type RetentionHoldRepository interface {
	// Place records a hold on an instance of the caller's, inside the caller's
	// transaction. An instance that is not there, or is not the caller's, is
	// not found.
	Place(ctx context.Context, hold entities.RetentionHold) (entities.RetentionHold, error)
	// Release removes the hold on an instance, inside the caller's
	// transaction, and returns what it was. No hold is not found.
	Release(ctx context.Context, instanceID uuid.UUID) (entities.RetentionHold, error)
	// ForInstance reads the hold on an instance; held is false when there is
	// none.
	ForInstance(ctx context.Context, instanceID uuid.UUID) (hold entities.RetentionHold, held bool, err error)
	// ListByProject lists a project's holds, newest first, at most limit.
	ListByProject(ctx context.Context, projectID uuid.UUID, limit int64) ([]entities.RetentionHold, error)
}
