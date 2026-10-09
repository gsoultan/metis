package impl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// defaultSweepLockWait is how long the closing of one request waits for a row
// somebody else holds before it gives that request up for this pass. The
// sweep runs in the server's retention loop, with that loop's other work
// behind it; a row held for longer than this is held by something that is not
// about to finish, and the next pass meets the request again.
const defaultSweepLockWait = 2 * time.Second

// defaultSweepBudget is how many closings one pass attempts before it stops
// and leaves the rest to the next. It is a count of attempts and not of seconds:
// the next pass starts again from the oldest request, and meets first every
// one that could not be closed — so a budget those could use up by being slow
// would be a pass that never gets beyond them. An attempt is one short
// transaction; ten thousand of them is a backlog no installation should see
// and still well under a pass's ten minutes, so in practice a pass ends
// because it has read everything.
const defaultSweepBudget = 10_000

// sweepLogsEach is how many of a pass's failures are logged one by one. The
// rest are counted: a store with a thousand requests that cannot be closed
// says so in one line, not a thousand every ten minutes.
const sweepLogsEach = 5

// sweepRead is one of the sweep's reads: up to limit requests the clock has
// closed, in order, strictly after a cursor, each row held.
type sweepRead func(ctx context.Context, after repocontracts.SweepCursor, limit int) ([]entities.DeviationRequest, error)

// sweepTally is what one pass over one of the sweep's reads did.
type sweepTally struct {
	// closed and failed count the requests the pass closed and the requests
	// it tried to close and could not. A request somebody else decided first
	// is neither.
	closed, failed int
	// first is the first failure, with the request it was of.
	first error
	// spent says the pass stopped because it had made as many attempts as a
	// pass may. Whether anything was left it did not read on to find out.
	spent bool
	// stopped is what ended the pass early: a failure to read, or its context.
	stopped error
}

// sweep makes one pass over a read: every request the read answers, oldest
// deadline first, each closed in a unit of work of its own.
//
// It keeps a cursor at the last request it was answered and reads the one
// after it (sweepNext), so a request that could not be closed is passed and
// not met again in this pass — however many such requests there are, and
// wherever they stand. The pass ends when a read answers nothing, when it has
// made as many attempts as a pass may (defaultSweepBudget), or when it cannot
// read at all.
//
// No lock is held from one request to the next, and a request that fails
// undoes nothing but its own closing. A request another transaction holds is
// not answered by the read at all — it is being decided now — and the cursor
// moves beyond it with the rest; the next pass meets it.
//
// The first sweepLogsEach failures are logged as they happen, each naming its
// request; all are counted, for the caller to say at the end.
func (s *deviationRequestService) sweep(ctx context.Context, read sweepRead, closeOne func(context.Context, entities.DeviationRequest) error) sweepTally {
	var tally sweepTally
	var cursor repocontracts.SweepCursor
	for attempts := 0; ; attempts++ {
		if err := ctx.Err(); err != nil {
			tally.stopped = err
			return tally
		}
		if attempts == s.sweepBudget {
			tally.spent = true
			return tally
		}
		request, found, closed, err := s.sweepNext(ctx, cursor, read, closeOne)
		if !found {
			tally.stopped = err
			return tally
		}
		cursor = repocontracts.SweepCursorAt(request)
		switch {
		case closed:
			tally.closed++
		case err != nil:
			tally.failed++
			if tally.first == nil {
				tally.first = fmt.Errorf("request %s: %s", request.ID, err.Error())
			}
			if tally.failed <= sweepLogsEach {
				log.Warn().Str("request", request.ID.String()).Str("error", err.Error()).
					Msg("A request the clock has closed could not be written down as closed, and was left as it was. " +
						"It reads as closed and nobody can act on it, but its row still says otherwise and it still holds what it was asked for.")
			}
		}
	}
}

// sweepNext closes the first request a read answers after the cursor, in one
// unit of work, and answers that request and what became of it.
//
// The unit of work first bounds its own wait for a lock (BoundLockWait): the
// read passes by a request somebody holds, but what the closing needs next —
// the ledger row that waits on a waive's request — can be held by somebody
// else, and without a bound the pass would stand behind them.
//
// found false means the read answered nothing: the pass is over, or — with an
// error — could not read. With a request found, an error is that request's
// own, and everything its closing wrote has been undone. A request found
// decided all the same (it cannot be, with its row held) is answered as not
// closed, with no error.
func (s *deviationRequestService) sweepNext(
	ctx context.Context,
	after repocontracts.SweepCursor,
	read sweepRead,
	closeOne func(context.Context, entities.DeviationRequest) error,
) (request entities.DeviationRequest, found, closed bool, err error) {
	err = s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		if err := s.repo.DeviationRequest().BoundLockWait(txCtx, s.sweepLockWait); err != nil {
			return err
		}
		next, err := read(txCtx, after, 1)
		if err != nil || len(next) == 0 {
			return err
		}
		request, found = next[0], true
		err = closeOne(txCtx, request)
		if errors.Is(err, repocontracts.ErrDeviationRequestDecided) {
			return nil
		}
		closed = err == nil
		return err
	})
	return request, found, closed && err == nil, err
}
