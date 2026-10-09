package impl

import (
	"context"
	"fmt"

	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// expireMigrationRequest records that a request for a migration passed its
// deadline undecided. Its caller holds the request's row and has found it
// still waiting and overdue.
//
// The request's own row is the whole record. A migration's request is for a
// version, not an instance: no ledger row waits on it, and nothing was done
// to any instance whose trail could say that a request about its version
// expired. The request is never deleted, and reads expired — with who asked,
// for what and until when — to every administrator of its organization.
//
// It is what the sweep closes such a request with, what an approval or a
// rejection that finds one overdue closes it with, and what asking again
// closes it with.
func expireMigrationRequest(ctx context.Context, requests repocontracts.DeviationRequestRepository, request entities.DeviationRequest) error {
	_, err := requests.Transition(ctx, request.ID, entities.DeviationRequestPending,
		repocontracts.DeviationRequestChange{Status: entities.DeviationRequestExpired})
	if err != nil {
		return fmt.Errorf("closing request %s as %s: %w", request.ID, entities.DeviationRequestExpired, err)
	}
	return nil
}

// unreportedRuns names, for the sweep's words, the approved requests whose
// run said nothing by the time its window closed: "3 requests whose approved
// run never reported could not be closed".
const unreportedRuns = "whose approved run never reported"

// runNeverReported is the sweep's word, in a request's outcome, for an
// approved request whose run said nothing by the time its window closed.
const runNeverReported = "the run did not report back"

// unreportedOutcome is the outcome the sweep's mark leaves on a request: that
// the run did not report back, and — carried from the outcome it replaces —
// what a self-approval rested on. It holds no count of what a run did, which
// is how a run that was in fact still going tells this mark from a report
// (reportedByARun), and writes its own over it.
func unreportedOutcome(stored map[string]any) map[string]any {
	outcome := carriedSelfApproval(stored)
	outcome[outcomeError] = runNeverReported
	return outcome
}

// interruptUnreported records that an approved request's run never reported
// back: the request is interrupted, and holds its migration no longer. Its
// caller holds the request's row and has found it approved with its run
// window closed.
//
// A report says what happened and names nobody: who approved, from which
// account, why and when stay as the approval wrote them. The request's own
// row is the record, as it is of an expiry.
//
// It is what the sweep closes such a request with, and what asking again
// closes it with.
func interruptUnreported(ctx context.Context, requests repocontracts.DeviationRequestRepository, request entities.DeviationRequest) error {
	_, err := requests.Transition(ctx, request.ID, entities.DeviationRequestApproved, repocontracts.DeviationRequestChange{
		Status: entities.DeviationRequestInterrupted, Outcome: unreportedOutcome(request.Outcome),
	})
	if err != nil {
		return fmt.Errorf("closing request %s as %s: %w", request.ID, entities.DeviationRequestInterrupted, err)
	}
	return nil
}
