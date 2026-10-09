package impl

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
)

// gateRefusal is the gate's refusal of an apply: forbidden, and nothing was
// moved. why is the reason in a few words of the server's own, for the
// request's record when the apply refused was an approved run.
type gateRefusal struct {
	err error
	why string
}

func (g gateRefusal) Error() string { return g.err.Error() }

func (g gateRefusal) Unwrap() error { return g.err }

// refusedAtTheGate builds a refusal: who may not is forbidden (Ruling 12), a
// plan nobody else approved is not permitted rather than malformed.
func refusedAtTheGate(why string, format string, args ...any) error {
	return gateRefusal{err: apierr.Forbiddenf(format, args...), why: why}
}

// errApprovedRunInsideTransaction is what an apply under a request answers
// when it is started inside a transaction: the gate takes the request's row
// to read it, and would then hold it for as long as that transaction lasts —
// across the whole run, and before every instance the caller had locked.
var errApprovedRunInsideTransaction = errors.New(
	"a migration under an approved request cannot run inside another transaction: " +
		"the request's row would be held across the whole run")

// approvedRequestFor is the gate of ApplyInstanceMigration: it answers the
// approval this apply runs under, or refuses the apply.
//
// It stands after the plan the apply runs from and before anything is
// written, and it only verifies. It makes no request and changes none, so
// neither a dry run — which never comes here — nor a refused apply leaves
// anything behind.
//
// With no request offered, a plan that needs nobody else passes with no
// approval. A request that is offered is always verified, whether or not the
// plan needs one: an apply never runs under a request it was not shown to be
// covered by.
//
// What is verified is the stored request, read under its row's lock so that
// a decision or a report being written at this moment is waited for and then
// read — never the caller's word. The lock is let go before this returns:
// the run that follows takes its instances one at a time and holds no
// request. The request has to be a migration's, approved, with its run
// window still open — asked of the clock here, so the bound on an approved
// run does not rest on a sweep having run — asked for by whoever this apply
// names as authorising it, and to cover the migration (whyNoLongerHolds):
// the same policy, over no running instance the request did not show. The
// plan and the listing compared are the very ones apply is handed.
//
// Whatever approval the caller put in the options is not read beyond its
// request id: what comes back is built from the stored request alone.
func (s *migrationService) approvedRequestFor(
	ctx context.Context,
	sourceDefID, targetDefID uuid.UUID,
	nodeMapping map[string]string,
	options servicecontracts.MigrationOptions,
	plan entities.MigrationPlan,
	covered []models.ProcessInstanceModel,
) (servicecontracts.MigrationApproval, error) {
	var none servicecontracts.MigrationApproval
	requestID := options.Approval.RequestID
	if requestID == uuid.Nil {
		if !plan.RequiresSecondApprover {
			return none, nil
		}
		return none, refusedAtTheGate("nobody else had approved it",
			"This migration skips a step or drops a control (%s), so a second administrator has to approve it first; nothing was moved.",
			strings.Join(plan.SecondApproverReasons, "; "))
	}
	request, err := s.requestOffered(ctx, requestID)
	if err != nil {
		return none, err
	}
	if request.Kind != entities.DeviationRequestMigration || request.Status != entities.DeviationRequestApproved {
		return none, refusedAtTheGate("the request was no longer approved when the run came to start",
			"request %s is %s, not an approved migration; nothing was moved", requestID, request.Status)
	}
	if request.RunWindowClosed(time.Now()) {
		return none, refusedAtTheGate("the time an approved run is given had passed before the run started",
			"request %s was approved on %s and its run did not report back, so it is no longer in use; nothing was moved — ask again",
			requestID, decidedOn(request).UTC().Format(decidedOnLayout))
	}
	if options.Actor != request.RequestedBy {
		return none, refusedAtTheGate("the run named somebody other than the requester as having authorised it",
			"request %s was asked for by %s, and this run names somebody else as having authorised it; nothing was moved",
			requestID, request.RequestedBy)
	}
	fingerprint := migrationFingerprint(sourceDefID, targetDefID, nodeMapping, options, plan.ComplianceHolds)
	if why := whyNoLongerHolds(request, fingerprint, activeInstanceIDs(covered)); why != "" {
		return none, refusedAtTheGate(why, "request %s does not cover this migration: %s; nothing was moved — ask again", requestID, why)
	}
	return verifiedApproval(request)
}

// requestOffered reads the request an apply offers, under its row's lock, in
// a unit of work of its own that ends before the run begins.
func (s *migrationService) requestOffered(ctx context.Context, requestID uuid.UUID) (entities.DeviationRequest, error) {
	var request entities.DeviationRequest
	requests := s.repo.DeviationRequest()
	if requests == nil {
		// The server's fault, as it is to whoever asks or approves: a plain
		// error, not something this caller may not do. Nothing was moved.
		return request, errNoDeviationRequests
	}
	if db.InTransaction(ctx) {
		return request, errApprovedRunInsideTransaction
	}
	err := s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		var err error
		request, err = requests.GetForUpdate(txCtx, requestID)
		return err
	})
	if errors.Is(err, apierr.ErrNotFound) {
		return request, refusedAtTheGate("the request was not found when the run came to start",
			"request %s does not exist here; nothing was moved", requestID)
	}
	if err != nil {
		return request, fmt.Errorf("reading request %s, which this migration was to run under: %w", requestID, err)
	}
	return request, nil
}

// verifiedApproval is the approval a stored, approved request records, as
// the run's writers are handed it.
//
// A request its own requester approved has to say which organization that
// was allowed in: the approval stored it, and the run asks nobody again. One
// that does not is not run under — its records would name no organization.
func verifiedApproval(request entities.DeviationRequest) (servicecontracts.MigrationApproval, error) {
	var none servicecontracts.MigrationApproval
	approval := servicecontracts.MigrationApproval{
		RequestID: request.ID, RequestedBy: request.RequestedBy, RequestedByID: request.RequestedByID,
		ApprovedBy: request.DecidedBy, ApprovedByID: request.DecidedByID, DecidedAt: decidedOn(request),
		SelfApproved: request.SelfApproved(),
	}
	if !approval.Granted() || request.RequestedByID == uuid.Nil {
		return none, refusedAtTheGate("the request did not say who asked for it and who approved it",
			"request %s does not say from which accounts it was asked for and approved; nothing was moved", request.ID)
	}
	if !approval.SelfApproved {
		return approval, nil
	}
	said, isText := request.Outcome[auditOrganizationID].(string)
	organization, err := uuid.Parse(said)
	if !isText || err != nil || organization == uuid.Nil {
		return none, refusedAtTheGate("the request did not say which organization its requester's own approval was allowed in",
			"request %s was approved by whoever asked for it, and does not say in which organization that was allowed; nothing was moved", request.ID)
	}
	approval.Organization = organization
	return approval, nil
}
