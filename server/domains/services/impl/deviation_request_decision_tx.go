package impl

import (
	"context"
	"errors"
	"fmt"

	"github.com/gsoultan/metis/internal/pkg/apierr"

	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
)

// committedRefusal is a refusal whose record has to be kept.
//
// A decision that finds its request past its deadline, or no longer true of
// the instance, refuses — and what it found is itself something the ledger
// keeps: the request is expired, or stale, from that moment. Returned from a
// unit of work as any error is, the refusal would undo that record with
// everything else. So the work wraps it in this, and runDecision commits and
// then refuses.
type committedRefusal struct{ cause error }

func (r committedRefusal) Error() string { return r.cause.Error() }

func (r committedRefusal) Unwrap() error { return r.cause }

// refuseAfterCommit marks cause as a refusal to give once what was written
// before it has been committed. It is for a refusal that follows a record of
// why, and for nothing else: whatever the unit of work wrote is kept.
func refuseAfterCommit(cause error) error {
	return committedRefusal{cause: cause}
}

// errDecisionInsideTransaction is what a decision answers when it is started
// inside a transaction somebody else opened. A plain error: it is how the
// code that called it was written, and nothing a caller of the API can fix.
var errDecisionInsideTransaction = errors.New(
	"a decision on a request for a second administrator cannot run inside another transaction: " +
		"what it records before it refuses would be undone with that transaction, and its locks would be taken after that transaction's")

// runDecision runs work in one unit of work of its own.
//
// A committedRefusal from work commits what work wrote, and its cause is then
// returned. Any other error rolls everything back and is returned as it is.
// If the commit itself fails, that failure is returned: nothing was kept, and
// the refusal would say something had been.
//
// Of its own: started inside a transaction its caller holds, the unit of work
// would join that one, and nothing here would commit. A request closed as
// stale or expired and then refused would be undone when the caller's
// transaction ended on that refusal — the caller told it had been recorded.
// And whatever the caller had locked would be held before the request's row,
// which every decision takes first. So it refuses, having done nothing.
func runDecision(ctx context.Context, uow repocontracts.UnitOfWork, work func(txCtx context.Context) error) error {
	if db.InTransaction(ctx) {
		return errDecisionInsideTransaction
	}
	var refusal error
	err := uow.Do(ctx, func(txCtx context.Context) error {
		err := work(txCtx)
		var kept committedRefusal
		if errors.As(err, &kept) {
			refusal = kept.cause
			return nil
		}
		return err
	})
	if err != nil {
		return err
	}
	return refusal
}

// writeFailed is the failure of a write a decision makes on a request it
// already holds, or on what waits on that request, told with what it was
// doing.
//
// The cause stays under it, so that a repository's "somebody decided this
// first" is still found for what it is (decidedFirst) — unless the cause is
// "not found". The request was found: its row is held. A row that then
// cannot be found under it is the server's trouble, and carried out as it
// came it would answer the approver that the request they are looking at
// does not exist. So that one is kept as words only, as a missing version is
// (graphRunBy).
func writeFailed(doing string, err error) error {
	if errors.Is(err, apierr.ErrNotFound) {
		return effectFailed(doing, err)
	}
	return fmt.Errorf("%s: %w", doing, err)
}
