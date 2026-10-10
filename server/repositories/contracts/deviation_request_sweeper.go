package contracts

import (
	"context"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// DeviationRequestSweeper reads what the clock has closed and nobody has
// written down yet. All of it is refused outside a transaction
// (ErrDeviationRequestOutsideTransaction): the reads hold the rows they
// answer until it ends, and pass by a row somebody else holds — that one is
// being decided, or reported on, right now. So a read can answer fewer than
// limit while more remain, and what it passed by is simply not answered: the
// cursor moves beyond it, and the next pass meets it. A row is moved with
// Transition, which writes only if the status is still the one read.
//
// The reads answer requests as List does — no Command, no Plan, no
// ApprovedInstances — because a sweep closes requests and has no use for what
// they asked, and one request whose sealed plan no longer opens must not fail
// the read it is in. Whoever needs a swept request's documents reads it with
// GetForUpdate. Run as system work they see every organization's requests, a
// deleted project's among them; run for one organization, only its own.
type DeviationRequestSweeper interface {
	// ListOverdue answers up to limit requests that still wait at or after
	// their deadline, the longest overdue first, strictly after the cursor.
	ListOverdue(ctx context.Context, now time.Time, after SweepCursor, limit int) ([]entities.DeviationRequest, error)

	// ListUnreported answers up to limit approved requests whose run window
	// has closed at now, as RunWindowClosed says — the deadline has come, the
	// report window has passed since the approval, or the approval has no
	// time at all — in the same order, strictly after the cursor.
	ListUnreported(ctx context.Context, now time.Time, after SweepCursor, limit int) ([]entities.DeviationRequest, error)

	// BoundLockWait makes the transaction it is called in give up on a row
	// lock it has waited that long for (SET LOCAL lock_timeout): the
	// statement that was waiting fails, and with it the transaction. It is how a sweep
	// closes one request without standing behind whoever holds a row that
	// request needs, with the rest of its pass behind it. A wait that is not
	// longer than nothing is a plain error.
	BoundLockWait(ctx context.Context, wait time.Duration) error
}
