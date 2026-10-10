package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
)

// overdueRequest is how an apply says that the visit it was asked to act on
// is held by a request that holds nothing any longer: one past its deadline
// that nobody has yet recorded as expired, or one that was decided while the
// apply was reading it.
//
// It is an error only so that it ends the apply's unit of work, which lets go
// of the instance. It never reaches a caller of the service: DeviateInstance
// reads it, closes the request, and applies once more.
type overdueRequest struct{ id uuid.UUID }

func (o overdueRequest) Error() string {
	return fmt.Sprintf("request %s no longer holds its step: it is past its deadline, or was decided as it was read", o.id)
}

// closeOverdue records that a request for a waive passed its deadline with
// nobody having decided it, so that the step it held can be asked for again
// now and not when the sweep next comes round.
//
// It is a unit of work of its own (runDecision), and takes the request's row
// and no instance: the apply that met the request has ended and let go of
// its own, which is what keeps the order — a request's row, then an
// instance's — true of an ask as it is of a decision. Started while an
// instance is still held, inside the apply's transaction, it refuses.
//
// What it writes is what the sweep writes for the request, through the same
// function (expireWaive), under the same row. So the two can meet: whichever
// has the row closes the request, and the other finds it closed.
//
// The request is read without its sealed documents: closing it needs none of
// them, so one whose stored plan no longer opens does not hold its step for
// ever.
//
// A request that is no longer waiting, or — read again under its row — not
// past its deadline, is left alone: somebody decided it meanwhile, and the
// apply that follows finds what they left. A failure is the server's,
// whatever class it came with: the caller found this request through an
// instance of their own, so "not found" is not an answer about it.
func (s *instanceDeviationService) closeOverdue(ctx context.Context, id uuid.UUID) error {
	requests := s.repo.DeviationRequest()
	if requests == nil || s.repo.DeviationDecider() == nil {
		return errNoDeviationRequests
	}
	return runDecision(ctx, s.repo.UnitOfWork(), func(txCtx context.Context) error {
		request, err := requests.GetForUpdateWithoutDocuments(txCtx, id)
		if err != nil {
			return effectFailed(fmt.Sprintf("reading request %s, which is past its deadline", id), err)
		}
		// Asked of the row as stored first: one already written down as
		// expired reads as expired too, and is not closed a second time.
		if request.Kind != entities.DeviationRequestInstanceWaive || request.Status != entities.DeviationRequestPending ||
			request.EffectiveStatus(time.Now()) != entities.DeviationRequestExpired {
			return nil
		}
		if err := s.expireWaive(txCtx, request); err != nil {
			return effectFailed(fmt.Sprintf("recording that request %s expired", id), err)
		}
		return nil
	})
}
