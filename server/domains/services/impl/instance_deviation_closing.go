package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/metis/server/domains/entities"
)

// expireWaive records that a request for a waive reached its deadline with
// nobody having decided it: the request and the ledger row that waited on it
// read expired, the row lets go of its visit, and the trail says so. Nothing
// about the instance changes.
//
// It is the one way an expiry is written — by the approval or the rejection
// that finds the request overdue, by an ask that finds it holding its step,
// and by the sweep — so whichever comes first, the record is the same. Its
// caller holds the request's row and has found it still waiting and past its
// deadline; a request somebody else decided first comes back as the
// repository's refusal (ErrDeviationRequestDecided), for the caller to tell.
//
// It needs no more of the request than the sweep reads: its id, its instance,
// the visit it holds, who asked and its deadline. The step is named from the
// ledger row, not from the request's stored plan, which the sweep never
// opens. Nobody is named as having decided, and the row is dated by the
// deadline, as the request is: that is when it happened, whenever it was
// found.
func (s *instanceDeviationService) expireWaive(ctx context.Context, request entities.DeviationRequest) error {
	deadline := request.ExpiresAt.UTC().Format(decidedOnLayout)
	_, err := s.settleWaiveRequest(ctx, request, entities.DeviationExpired, entities.DeviationRequestExpired, EventDeviationExpired,
		func(step string) string {
			return fmt.Sprintf("Nobody decided the request to waive “%s” that %s made before it expired on %s, so nothing changed.",
				step, request.RequestedBy, deadline)
		}, entities.DeviationDecision{}, nil)
	return err
}

// rejectWaive ends a request for a waive because an administrator said no,
// and answers the request as it then is. The request and its ledger row read
// rejected, the row lets go of its visit — the same waive can be asked for
// again — and the trail says who ended it and why. Nothing about the instance
// changes, and no instance is read.
//
// Whoever asked may end their own request: that is a withdrawal, and the
// trail says so in those words, and marks it. Anybody else rejected it. Both
// are a rejection on the request and on the row, which name who did it.
//
// Its caller holds the request's row, and has found it still waiting and
// within its deadline.
func (s *instanceDeviationService) rejectWaive(
	ctx context.Context,
	request entities.DeviationRequest,
	decision entities.DeviationDecision,
) (entities.DeviationRequest, error) {
	return s.settleWaiveRequest(ctx, request, entities.DeviationRejected, entities.DeviationRequestRejected, EventDeviationRejected,
		func(step string) string {
			if decision.DeciderID == request.RequestedByID {
				return fmt.Sprintf("%s withdrew their request to waive “%s”, so nothing changed. Reason: %s", decision.Decider, step, decision.Reason)
			}
			return fmt.Sprintf("%s rejected the request to waive “%s” that %s made, so nothing changed. Reason: %s",
				decision.Decider, step, request.RequestedBy, decision.Reason)
		}, decision, nil)
}
