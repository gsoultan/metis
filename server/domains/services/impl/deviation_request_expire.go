package impl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
)

// expirySweepBatch is how many overdue requests one batch of the sweep
// closes before the pass looks at whether to go on.
const expirySweepBatch = 100

// expirySweepGivesUpAfter is how many requests one batch may fail to close
// before it stops. Each one a batch has set aside has to be read past to
// reach the next, so the number is small: a store with this many requests
// that cannot be closed needs a person, not a longer read on every pass.
const expirySweepGivesUpAfter = 20

// errExpiryIsTheServers is what the sweep answers anybody but the server. A
// plain error: nothing a client sends can reach it, so it is how the code
// that called it was written.
var errExpiryIsTheServers = errors.New(
	"recording which requests have expired is the server's own work, on the server's clock; nothing a request carries may ask for it")

// errExpiryInsideTransaction is what the sweep answers when it is started
// inside a transaction: it closes each request in one of its own.
var errExpiryInsideTransaction = errors.New(
	"the expiry of requests cannot run inside another transaction: each request is closed in a transaction of its own")

// ExpireDeviationRequests writes down what the clock has decided — every
// request still waiting at now that is past its deadline — and answers how
// many it closed.
//
// It is the server's own work and refuses anybody else: now is its caller's,
// and whoever could choose it could expire every request there is. Run as
// system work it reaches every organization's requests, a deleted project's
// among them.
//
// It closes what it can and goes on. A request that cannot be closed is left
// exactly as it was and said (expireBatch); the error answered then names the
// first of them, beside the count of those that were closed. A failure to
// read at all ends the pass.
//
// Every replica runs it. Two passes at once close each request once: each
// takes the row of the request it is closing, and passes by a row somebody
// else holds.
func (s *deviationRequestService) ExpireDeviationRequests(ctx context.Context, now time.Time) (int64, error) {
	if !entities.IsSystemContext(ctx) {
		return 0, errExpiryIsTheServers
	}
	if db.InTransaction(ctx) {
		return 0, errExpiryInsideTransaction
	}
	if s.repo.DeviationRequest() == nil || s.repo.DeviationDecider() == nil {
		return 0, errNoDeviationRequests
	}
	var total int64
	var failure error
	for {
		closed, err := s.expireBatch(ctx, now)
		total += int64(closed)
		if failure == nil {
			failure = err
		}
		// A batch that closed fewer than it may has run out of requests it
		// can take — or could not read them, which its error says.
		if closed < expirySweepBatch {
			return total, failure
		}
	}
}

// expireBatch closes up to expirySweepBatch overdue requests and answers how
// many it closed.
//
// Each request is closed in a unit of work of its own (expireNext), so no
// lock is held from one request to the next, and a request that cannot be
// closed undoes nothing but its own closing. Such a request is logged, set
// aside so that this batch does not meet it again, and left: the batch goes
// on to the requests behind it. When it has set aside expirySweepGivesUpAfter
// it stops. Its error is then that some requests could not be closed, naming
// the first; a failure to read the overdue requests at all is returned as it
// is.
//
// A request another transaction holds is passed by, not waited for, so
// closing fewer than a batch does not mean none remain: what was passed by is
// being decided now, or is met by the next pass.
func (s *deviationRequestService) expireBatch(ctx context.Context, now time.Time) (int, error) {
	closed := 0
	aside := map[uuid.UUID]struct{}{}
	var failed int
	var first error
	for closed < expirySweepBatch && len(aside) < expirySweepGivesUpAfter {
		if err := ctx.Err(); err != nil {
			return closed, err
		}
		id, done, err := s.expireNext(ctx, now, aside)
		if id == uuid.Nil {
			if err != nil {
				return closed, err
			}
			break
		}
		if done {
			closed++
			continue
		}
		aside[id] = struct{}{}
		if err == nil {
			// Somebody decided it first: there is nothing to close.
			continue
		}
		failed++
		log.Warn().Str("request", id.String()).Str("error", err.Error()).
			Msg("A request past its deadline could not be closed, and was left as it was. It reads as expired and nobody can approve it, " +
				"but its row says pending and it still holds what it was asked for until it is repaired.")
		if first == nil {
			first = fmt.Errorf("request %s is past its deadline and could not be closed: %s", id, err.Error())
		}
	}
	if failed > 1 {
		return closed, fmt.Errorf("%d requests past their deadline could not be closed; the first: %s", failed, first.Error())
	}
	return closed, first
}

// expireNext closes the longest-overdue request that is not set aside and
// that nobody holds, in one unit of work, and answers which request that was
// and whether it was closed.
//
// The read takes the row it answers and passes by any that is held (FOR
// UPDATE SKIP LOCKED): a held request is being approved, rejected or closed
// by somebody else right now, and what they write stands. It reads one more
// row than are set aside, so there is one to close whenever one is free; the
// rows set aside are held for as long as this takes and no longer.
//
// A nil id says there is nothing left to take. An error beside a nil id is a
// failure to read; beside an id it is that request's own, and everything its
// closing wrote has been undone. A request found decided all the same — it
// cannot be, with its row held — is answered as not closed, with no error.
func (s *deviationRequestService) expireNext(ctx context.Context, now time.Time, aside map[uuid.UUID]struct{}) (id uuid.UUID, closed bool, err error) {
	err = s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		overdue, err := s.repo.DeviationRequest().ListOverdue(txCtx, now, len(aside)+1)
		if err != nil {
			return err
		}
		for _, request := range overdue {
			if _, skip := aside[request.ID]; skip {
				continue
			}
			id = request.ID
			err := s.expire(txCtx, request)
			if errors.Is(err, repocontracts.ErrDeviationRequestDecided) {
				return nil
			}
			closed = err == nil
			return err
		}
		return nil
	})
	return id, closed && err == nil, err
}

// expire records that one request passed its deadline undecided. Its caller
// holds the request's row and has found it still waiting and overdue.
//
// A waive's request takes its ledger row with it and says so on the
// instance's trail (expireWaive). A migration's is for a version: its own row
// is the whole record.
func (s *deviationRequestService) expire(ctx context.Context, request entities.DeviationRequest) error {
	switch request.Kind {
	case entities.DeviationRequestInstanceWaive:
		return s.waives.expireWaive(ctx, request)
	case entities.DeviationRequestMigration:
		_, err := s.repo.DeviationRequest().Transition(ctx, request.ID, entities.DeviationRequestPending,
			repocontracts.DeviationRequestChange{Status: entities.DeviationRequestExpired})
		if err != nil {
			return fmt.Errorf("closing request %s as %s: %w", request.ID, entities.DeviationRequestExpired, err)
		}
		return nil
	}
	return fmt.Errorf("request %s is a %s, which nothing here closes", request.ID, request.Kind)
}
