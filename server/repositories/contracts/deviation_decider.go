package contracts

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// ErrDeviationRowDecided is what Decide answers for a ledger row that does
// not wait for approval: it was decided once — or never waited — and what it
// says stands.
var ErrDeviationRowDecided = errors.New("this ledger row was already decided")

// LedgerRowDecision is what becomes of a ledger row that waited for approval.
//
// Status is where the row goes — applied, rejected, expired or stale — and
// DecidedAt is when; both are always given. A decision to applied also gives
// ApprovedBy and ApprovedByID: somebody approved it. For the other three they
// are written as given, so a row nobody approved names nobody. AuditEntryID, Task,
// Before, After and Details are left as stored when they are zero or nil: a
// waive that withdrew exactly one task names it on its row, and a row that
// was only waiting names none. A map that is empty and not nil replaces the
// stored one with nothing.
type LedgerRowDecision struct {
	Status       entities.DeviationStatus
	ApprovedBy   string
	ApprovedByID uuid.UUID
	DecidedAt    time.Time
	AuditEntryID uuid.UUID
	Task         *entities.Task

	Before, After, Details map[string]any
}

// DeviationDecider decides a ledger row that waits for approval, once.
type DeviationDecider interface {
	// Decide moves a row from pending_approval to decision.Status and answers
	// the row as it then is. It is refused outside a transaction
	// (ErrDeviationOutsideTransaction), and it holds the row while it
	// decides, so of two decisions made at once one is written and the other
	// is ErrDeviationRowDecided. A decision back to pending_approval, to a
	// status outside the closed set, at no time, or to applied without saying
	// who approved and from which account, is a plain error.
	//
	// The row holds its visit only while it is live (DeviationStatus.Live:
	// applied, or waiting). Decided to applied it goes on holding it; decided
	// to anything else it lets go, and the same thing can be asked for again.
	//
	// Whoever decides a row holds its request's row already: request, then
	// instance, then task rows, then this.
	Decide(ctx context.Context, id uuid.UUID, decision LedgerRowDecision) (entities.Deviation, error)
}
