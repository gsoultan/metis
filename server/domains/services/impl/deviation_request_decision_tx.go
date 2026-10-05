package impl

import (
	"context"
	"errors"

	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
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

// runDecision runs work in one unit of work.
//
// A committedRefusal from work commits what work wrote, and its cause is then
// returned. Any other error rolls everything back and is returned as it is.
// If the commit itself fails, that failure is returned: nothing was kept, and
// the refusal would say something had been.
func runDecision(ctx context.Context, uow repocontracts.UnitOfWork, work func(txCtx context.Context) error) error {
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
