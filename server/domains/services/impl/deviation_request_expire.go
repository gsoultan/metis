package impl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
)

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
// It is one pass (sweep): every overdue request is read once, oldest first,
// and closed in a transaction of its own. A request that cannot be closed is
// left exactly as it was and passed; the requests behind it are still
// reached, however many such requests head the line. Whatever a pass leaves
// behind it says (tallied): in one line of the log, and in the error it
// answers beside the count of what it did close.
//
// Every replica runs it. Two passes at once close each request once: each
// takes the row of the request it is closing, and is not answered a row
// somebody else holds.
func (s *deviationRequestService) ExpireDeviationRequests(ctx context.Context, now time.Time) (int64, error) {
	if !entities.IsSystemContext(ctx) {
		return 0, errExpiryIsTheServers
	}
	if db.InTransaction(ctx) {
		return 0, errExpiryInsideTransaction
	}
	requests := s.repo.DeviationRequest()
	if requests == nil || s.repo.DeviationDecider() == nil {
		return 0, errNoDeviationRequests
	}
	overdue := s.sweep(ctx, func(txCtx context.Context, after repocontracts.SweepCursor, limit int) ([]entities.DeviationRequest, error) {
		return requests.ListOverdue(txCtx, now, after, limit)
	}, s.expire)
	return int64(overdue.closed), tallied("past their deadline", overdue)
}

// tallied says what a pass left behind, and answers it as the pass's error:
// nil when it closed everything it read and read everything there was.
//
// A pass that left something says so in one line of the server's log — how
// many it closed, how many it could not, and whether it stopped short — so
// that a pass which did not finish never looks like one with nothing to do.
// what names the requests the pass was over, for the words.
//
// A pass that used its budget and failed on nothing is not an error: it did
// what it was allowed, and the next pass goes on. It is still said.
func tallied(what string, tally sweepTally) error {
	if tally.failed == 0 && !tally.spent && tally.stopped == nil {
		return nil
	}
	line := log.Warn().Int("closed", tally.closed).Int("not_closed", tally.failed).Bool("budget_spent", tally.spent)
	if tally.stopped != nil {
		line = line.Str("stopped_by", tally.stopped.Error())
	}
	line.Msg("A pass over the requests " + what + " left some as they were; the next pass meets them again")
	switch {
	case tally.stopped != nil && tally.failed == 0:
		return tally.stopped
	case tally.stopped != nil:
		return fmt.Errorf("%s (and before that, %s %s could not be closed; the first: %s)",
			tally.stopped.Error(), counted(tally.failed), what, tally.first.Error())
	case tally.failed == 0:
		return nil
	}
	return fmt.Errorf("%s %s could not be closed (%d closed); the first: %s", counted(tally.failed), what, tally.closed, tally.first.Error())
}

// counted is so many requests, in words a sentence can use.
func counted(requests int) string {
	if requests == 1 {
		return "1 request"
	}
	return fmt.Sprintf("%d requests", requests)
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
