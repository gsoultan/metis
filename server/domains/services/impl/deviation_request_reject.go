package impl

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// RejectDeviationRequest ends a request without carrying it out, and answers
// it as it then is.
//
// Its order is its own, not an approval's, because who may reject is not who
// may approve:
//
//  1. Who is asking, before anything is read: a signed-in administrator of
//     the organization the request is for, with an account. Nobody else
//     learns whether there is such a request.
//  2. The reason, which a rejection always has: it is what the record says of
//     why a waive somebody asked for was not made.
//  3. The request, read inside the caller's organization and held (another
//     organization's is not found, as one that never existed), in a unit of
//     work of its own. It is read without what it asked for and what its
//     requester was shown: ending a request needs neither, so one whose
//     stored plan no longer opens can still be rejected or withdrawn.
//  4. Its status as stored on that row: one already decided says what became
//     of it.
//  5. The clock: one past its deadline expired before this rejection came.
//     That is recorded — and kept — and the rejection is refused.
//  6. The rejection. Any administrator of the organization may make it, the
//     requester among them: ending one's own request is a withdrawal, and
//     the record says which it was.
//
// No instance is read or locked at any step, and nothing about one changes.
func (s *deviationRequestService) RejectDeviationRequest(ctx context.Context, id uuid.UUID, reason string) (entities.DeviationRequest, error) {
	var none entities.DeviationRequest
	caller, err := requireDecidingAdministrator(ctx)
	if err != nil {
		return none, err
	}
	reason, err = rejectionReason(reason)
	if err != nil {
		return none, err
	}
	requests := s.repo.DeviationRequest()
	if requests == nil || s.repo.DeviationDecider() == nil {
		return none, errNoDeviationRequests
	}
	var rejected entities.DeviationRequest
	err = runDecision(ctx, s.repo.UnitOfWork(), func(txCtx context.Context) error {
		var err error
		rejected, err = s.rejectLocked(txCtx, id, caller, reason)
		// A decision a repository refused as made first is somebody else's,
		// told as that (decidedFirst), as an approval tells it.
		return decidedFirst(err, func() (entities.DeviationRequest, error) { return requests.GetReadable(txCtx, id) })
	})
	if err != nil {
		return none, err
	}
	// The rejection held the request without its documents, which it had no
	// use for; what it answers is the request as a single read answers it —
	// each document that opens, and only one that does not left out. A read
	// that fails here changes nothing about the rejection, which is made:
	// the request is then answered as the rejection held it.
	if whole, err := requests.GetReadable(ctx, id); err == nil {
		return whole, nil
	}
	return rejected, nil
}

// rejectionReason is the reason of a rejection as the record keeps it:
// without the spaces around it, never empty, and no longer than the ledger
// keeps a reason.
func rejectionReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", apierr.Invalidf("Say why: a rejection keeps its reason with the record.")
	}
	if utf8.RuneCountInString(reason) > entities.MaxDeviationReasonLength {
		return "", errDecisionReasonTooLong()
	}
	return reason, nil
}

// rejectLocked is RejectDeviationRequest inside its unit of work: steps 3 to
// 6. The moment of the decision is read once the row is held, so a rejection
// that waited for the row is judged against the deadline as it then stands.
func (s *deviationRequestService) rejectLocked(ctx context.Context, id uuid.UUID, caller entities.User, reason string) (entities.DeviationRequest, error) {
	var none entities.DeviationRequest
	request, err := s.repo.DeviationRequest().GetForUpdateWithoutDocuments(ctx, id)
	if err != nil {
		return none, err
	}
	if request.Status != entities.DeviationRequestPending {
		return none, decidedRefusal(request)
	}
	decision := entities.DeviationDecision{Decider: caller.Username, DeciderID: caller.ID, Reason: reason, At: time.Now()}
	if request.EffectiveStatus(decision.At) == entities.DeviationRequestExpired {
		if err := s.expire(ctx, request); err != nil {
			return none, err
		}
		return none, refuseAfterCommit(apierr.Invalidf("This request already expired on %s.", request.ExpiresAt.UTC().Format(decidedOnLayout)))
	}
	switch request.Kind {
	case entities.DeviationRequestInstanceWaive:
		return s.waives.rejectWaive(ctx, request, decision)
	case entities.DeviationRequestMigration:
		// A request for a migration is for a version, not an instance: there
		// is no ledger row that waits on it and no instance whose trail would
		// say it was rejected. The request's own row is the record.
		rejected, err := s.repo.DeviationRequest().Transition(ctx, request.ID, entities.DeviationRequestPending, repocontracts.DeviationRequestChange{
			Status: entities.DeviationRequestRejected, DecidedBy: decision.Decider, DecidedByID: decision.DeciderID,
			DecisionReason: decision.Reason, DecidedAt: decision.At,
		})
		if err != nil {
			return none, writeFailed(fmt.Sprintf("closing request %s as %s", request.ID, entities.DeviationRequestRejected), err)
		}
		return rejected, nil
	}
	// A kind nothing writes: the set is closed, and the kind of a request does
	// not change after it is made. Run by no test; kept so that a kind added
	// later without its rejection is told here.
	return none, fmt.Errorf("request %s is a %s, which nothing here rejects", request.ID, request.Kind)
}
