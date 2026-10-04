package contracts

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// ErrDeviationOutsideTransaction is what Create answers when no transaction is
// open. A deviation written apart from the change it records can outlive a
// change that rolled back, or be lost from one that committed.
var ErrDeviationOutsideTransaction = errors.New("a deviation is recorded in the transaction that makes the change it records, and none is open")

// DeviationRepository is the ledger of what was done to an instance that its
// process did not decide.
type DeviationRepository interface {
	// Create records one deviation. It is refused unless the context carries an
	// open transaction (ErrDeviationOutsideTransaction), and unless the
	// instance is in the project the deviation names and the project is the
	// caller's.
	Create(ctx context.Context, deviation entities.Deviation) (entities.Deviation, error)

	// ListByInstance returns every deviation an instance has, oldest first.
	ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]entities.Deviation, error)

	// FindLiveByVisit answers the row of an instance's visit that is applied or
	// awaiting approval. A caller acting on a visit asks first, so a retry
	// replays the row instead of acting twice.
	FindLiveByVisit(ctx context.Context, instanceID uuid.UUID, visitKey string) (entities.Deviation, bool, error)
}
