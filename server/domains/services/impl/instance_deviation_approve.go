package impl

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// requestStillWaits is what an approval adds when the waive it approved could
// not be applied for a reason the requester has to put right. The approver
// cannot change the values, and a corrected request is refused while this one
// waits — so they are told what to do, rather than left to find out by asking
// again.
const requestStillWaits = "The request is still waiting: reject it, and the waive can be asked for again."

// movedSinceAsked is why a request is stale when the instance is no longer
// where, or what, the request was made for.
const movedSinceAsked = "the instance has moved since it was asked for"

// approveWaive approves a request for a waive and applies it, in one unit of
// work: the waive is made and recorded as approved, or nothing is.
//
// The locks, in the order everything in this slice takes them: the request's
// row, then the instance's, then the rows of the step's open tasks (inside
// the effect, by id), then the ledger row that waited. Two approvals of one
// request meet at the request: the second waits, and then reads a request
// that has been decided. An approval and a completion of the step meet at the
// instance, and whichever is second finds what the first left.
//
// After the request is admitted it is apply, step for step, on the row it
// locks (approveLocked) — without the replay, because the row that waits is
// what a replay would find.
//
// A request found past its deadline, or no longer true of the instance, is
// recorded as expired or stale and then refused: that record is kept
// (runDecision). Any other failure undoes everything, and the request waits
// as it did.
//
// Holding the request's row is what makes a second decision read the first.
// The repositories refuse a second decision as well, whoever holds what; that
// refusal is answered as the request's own state (decidedFirst), so that the
// order of the locks is never the only thing between a double click and a
// server error.
//
// Who is approving is asked here, of whoever is signed in, whoever calls this
// and whatever they checked first: the approval is what acts, and a caller
// handed in as a value is a caller somebody could hand in.
func (s *instanceDeviationService) approveWaive(ctx context.Context, id uuid.UUID, reason string) (entities.DeviationRequestOutcome, error) {
	caller, err := requireDecidingAdministrator(ctx)
	if err != nil {
		return entities.DeviationRequestOutcome{}, err
	}
	if s.engine == nil {
		return entities.DeviationRequestOutcome{}, errDeviationWithoutEngine
	}
	if s.repo.DeviationRequest() == nil || s.repo.DeviationDecider() == nil {
		return entities.DeviationRequestOutcome{}, errNoDeviationRequests
	}
	var outcome entities.DeviationRequestOutcome
	err = runDecision(ctx, s.repo.UnitOfWork(), func(txCtx context.Context) error {
		var err error
		outcome, err = s.approveLocked(txCtx, id, caller, reason)
		// Whatever part of the approval a repository refused as decided
		// first — the waive's record, or the note that the request was stale
		// or expired — is somebody else's decision, told as that.
		return decidedFirst(err, func() (entities.DeviationRequest, error) {
			return s.repo.DeviationRequest().Get(txCtx, id)
		})
	})
	if err != nil {
		return entities.DeviationRequestOutcome{}, err
	}
	if outcome.Request.SelfApproved() {
		traceSelfApproval(outcome.Request)
	}
	return outcome, nil
}

// approveLocked is approveWaive inside its unit of work.
//
//  1. The request's row is taken and held. Whether it still waits, and who
//     may decide it, are asked of that row (approvalRules.admit): the
//     requester is refused here, having changed nothing — which is why the
//     attempt is said in the server's log, where alone it leaves a mark.
//  2. A request past its deadline is recorded as expired and refused. The
//     repository does not look at the clock; this does.
//  3. The command is read from the request, and is the request's own: it
//     names the request's instance, and the visit the request holds.
//  4. The instance's row is taken, once, and held to the end — as apply takes
//     it, and nowhere else here.
//  5. The instance is still running (refuseEnded). One that has ended makes
//     the request stale. One that is suspended is refused in refuseEnded's
//     own words and the request left waiting: it has not ended or moved, and
//     nothing asked for has changed.
//  6. The plan is made again from the locked row and is the plan the request
//     pinned, with nothing refusing it, and the instance still waits at the
//     step (refuseUnlessAsPreviewed). Otherwise the request is stale. The
//     plan the request stores is not read: it is what somebody was shown.
//  7. The waive (applyApproved).
func (s *instanceDeviationService) approveLocked(
	ctx context.Context,
	id uuid.UUID,
	caller entities.User,
	reason string,
) (entities.DeviationRequestOutcome, error) {
	var none entities.DeviationRequestOutcome
	request, err := s.repo.DeviationRequest().GetForUpdate(ctx, id)
	if err != nil {
		return none, err
	}
	if request.Kind != entities.DeviationRequestInstanceWaive {
		return none, fmt.Errorf("request %s is a %s, and was handed to the approval of a waive", request.ID, request.Kind)
	}
	decision, err := s.rules.admit(ctx, request, caller, reason)
	if err != nil {
		if caller.ID == request.RequestedByID && errors.Is(err, apierr.ErrForbidden) {
			traceRefusedSelfApproval(request, caller)
		}
		return none, err
	}
	if request.EffectiveStatus(decision.At) == entities.DeviationRequestExpired {
		return none, s.expiredAtApproval(ctx, request)
	}
	command, err := commandOf(request)
	if err != nil {
		return none, err
	}
	locked, err := s.repo.Process().GetForUpdate(ctx, command.InstanceID)
	if err != nil {
		// The request was found, so "not found" is not an answer about it.
		return none, effectFailed(fmt.Sprintf("reading the instance request %s is for", request.ID), err)
	}
	if err := refuseEnded(locked, command.Kind); err != nil {
		if locked.Status == models.ProcessSuspended {
			return none, err
		}
		return none, s.staleAtApproval(ctx, request, decision, fmt.Sprintf("the instance is %s", locked.Status), nil)
	}
	live := adapters.InstanceEntityAdapter{Model: locked}.ToEntity()
	plan, err := s.plan(ctx, live, command)
	if err != nil {
		return none, err
	}
	if err := refuseUnlessAsPreviewed(locked, plan, command); err != nil {
		return none, s.staleAtApproval(ctx, request, decision, whyStale(plan, command), plan.Refusals)
	}
	return s.applyApproved(ctx, request, decision, locked, &live, plan, command)
}

// commandOf is the waive a request asks for, as an apply of it: read from
// what the request stores, never a dry run, for the visit the request holds.
//
// A command that cannot be read, or that names another instance or another
// visit than its request does, is the server's trouble and a plain error —
// the request and the command were written together, by this server — and
// nothing is approved on it.
func commandOf(request entities.DeviationRequest) (entities.DeviationCommand, error) {
	command, err := waiveCommandFrom(request.Command)
	if err != nil {
		return entities.DeviationCommand{}, fmt.Errorf("request %s cannot be approved: %w", request.ID, err)
	}
	if request.Instance == nil || request.Instance.ID != command.InstanceID {
		return entities.DeviationCommand{}, fmt.Errorf("request %s cannot be approved: it is not for the instance its command names", request.ID)
	}
	if request.Fingerprint == "" || request.Fingerprint != command.VisitKey {
		return entities.DeviationCommand{}, fmt.Errorf("request %s cannot be approved: it does not hold the visit its command names", request.ID)
	}
	command.DryRun, command.VisitKey = false, request.Fingerprint
	return command, nil
}

// whyStale is why refuseUnlessAsPreviewed refused, said of a request: the
// plan's own refusals when the work is what was asked for and the plan now
// refuses it, and otherwise that the instance has moved — the visit is
// another, or the instance has left the step.
func whyStale(plan entities.DeviationPlan, command entities.DeviationCommand) string {
	if plan.VisitKey == command.VisitKey && !plan.Applicable() {
		return strings.TrimSuffix(strings.Join(plan.Refusals, " "), ".")
	}
	return movedSinceAsked
}

// applyApproved makes the waive an approved request asked for, and records it
// as approved.
//
// The effect is the one an apply made (waived), through the same code, so a
// failure is told the same way (waiveFailed): a gateway with no way out is a
// refusal in the same sentence — with what to do about a request that then
// still waits — and anything else is the server's, in the same words. Either
// way everything is undone and the request waits as it did; that an approval
// was given and came to nothing is said in the server's log, since nothing
// else keeps it.
//
// The row that waited becomes the row the effect made, whole: who held the
// work when it was withdrawn, cut and counted as a waive's row is, marked
// when the step is a control. Nothing is rebuilt here from the plan. It keeps
// its id, its run and its visit, and says who asked (its actor) and who
// approved. The trail gets the entry a waive always had, word for word, with
// the request and the approver in its data, and beside it an entry that says
// who approved.
func (s *instanceDeviationService) applyApproved(
	ctx context.Context,
	request entities.DeviationRequest,
	decision entities.DeviationDecision,
	locked models.ProcessInstanceModel,
	live *entities.ProcessInstance,
	plan entities.DeviationPlan,
	command entities.DeviationCommand,
) (entities.DeviationRequestOutcome, error) {
	var none entities.DeviationRequestOutcome
	def, err := s.graphRunBy(ctx, live.ID, uuid.UUID(locked.DefinitionID))
	if err != nil {
		return none, err
	}
	pending, err := s.rowWaitingOn(ctx, request)
	if err != nil {
		return none, err
	}
	row, control, err := s.waived(ctx, locked, live, def, plan, command, request.RequestedBy, pending.RunID)
	if err != nil {
		traceApprovalNotApplied(request, decision, errors.Is(err, apierr.ErrInvalidArgument))
	}
	if errors.Is(err, apierr.ErrInvalidArgument) {
		return none, fmt.Errorf("%w %s", err, requestStillWaits)
	}
	if err != nil {
		return none, err
	}
	decided, applied, err := s.recordApproved(ctx, request, decision, pending, row,
		waiveEntry(locked, plan, command, request.RequestedBy, pending.RunID, control))
	if err != nil {
		return none, err
	}
	return entities.DeviationRequestOutcome{Request: applied, Applied: true, Deviation: &decided, WaivePlan: &plan}, nil
}

// rowWaitingOn is the ledger row that waits on a request: the live row of the
// visit the request holds, on the request's instance. It is there, waiting,
// and names this request, or the ledger and the request disagree — which is
// the server's trouble, and nothing is approved on it.
func (s *instanceDeviationService) rowWaitingOn(ctx context.Context, request entities.DeviationRequest) (entities.Deviation, error) {
	if request.Instance == nil {
		return entities.Deviation{}, fmt.Errorf("request %s names no instance", request.ID)
	}
	store := s.repo.Deviation()
	if store == nil {
		return entities.Deviation{}, errNoDeviationLedger
	}
	row, found, err := store.FindLiveByVisit(ctx, request.Instance.ID, request.Fingerprint)
	if err != nil {
		return entities.Deviation{}, effectFailed(fmt.Sprintf("reading the ledger row of request %s", request.ID), err)
	}
	if !found || row.Status != entities.DeviationPendingApproval || row.RequestID != request.ID {
		return entities.Deviation{}, fmt.Errorf("request %s waits, and the ledger holds no row waiting on it", request.ID)
	}
	return row, nil
}

// recordApproved writes what an approved waive leaves: the row that waited,
// decided to applied and made the row the effect answered; the node_skipped
// entry, with the approval noted in its data; an entry that says who
// approved; and the request, applied. It answers the row and the request as
// the database then holds them.
//
// The ledger row is taken here, after the request, the instance and the
// tasks. An error comes back as it is, the repositories' "decided first"
// among them, for the caller to tell.
func (s *instanceDeviationService) recordApproved(
	ctx context.Context,
	request entities.DeviationRequest,
	decision entities.DeviationDecision,
	pending, row entities.Deviation,
	skipped entities.AuditEntry,
) (entities.Deviation, entities.DeviationRequest, error) {
	skippedID, err := uuid.NewV7()
	if err != nil {
		return entities.Deviation{}, entities.DeviationRequest{}, err
	}
	details := row.Details
	if decision.SelfApproved {
		details[auditSelfApproved] = true
		details[auditOtherAdministrators] = 0
	}
	decided, err := s.repo.DeviationDecider().Decide(ctx, pending.ID, repocontracts.LedgerRowDecision{
		Status: entities.DeviationApplied, ApprovedBy: decision.Decider, ApprovedByID: decision.DeciderID, DecidedAt: decision.At,
		AuditEntryID: skippedID, Task: row.Task, Before: row.Before, After: row.After, Details: details,
	})
	if err != nil {
		return entities.Deviation{}, entities.DeviationRequest{}, fmt.Errorf("recording that “%s” was waived: %w", stepOf(pending), err)
	}
	skipped = approvalNote(skipped, request, decision)
	skipped.ID = skippedID
	skipped.Data[auditDeviationID] = decided.ID.String()
	if err := s.writeEntries(ctx, decided, skipped, approvalEntry(request, decided, decision)); err != nil {
		return entities.Deviation{}, entities.DeviationRequest{}, err
	}
	applied, err := s.repo.DeviationRequest().Transition(ctx, request.ID, entities.DeviationRequestPending, repocontracts.DeviationRequestChange{
		Status: entities.DeviationRequestApplied, DecidedBy: decision.Decider, DecidedByID: decision.DeciderID,
		DecisionReason: decision.Reason, DecidedAt: decision.At, Outcome: map[string]any{auditDeviationID: decided.ID.String()},
	})
	if err != nil {
		return entities.Deviation{}, entities.DeviationRequest{}, fmt.Errorf("recording the approval of request %s: %w", request.ID, err)
	}
	return decided, applied, nil
}

// approvalEntry is the trail entry that says a request was approved, and by
// whom — or, for a self-approval, that no second person approved it. It is
// where the trail names the approver in words: the entry of what was done
// keeps the sentence it always had.
func approvalEntry(request entities.DeviationRequest, row entities.Deviation, decision entities.DeviationDecision) entities.AuditEntry {
	eventType := EventDeviationApproved
	narrative := fmt.Sprintf("%s approved %s's request to waive “%s”.", decision.Decider, request.RequestedBy, stepOf(row))
	if decision.Reason != "" {
		narrative += " Note: " + decision.Reason
	}
	if decision.SelfApproved {
		eventType = EventDeviationSelfApproved
		narrative = fmt.Sprintf("No second administrator approved this. %s approved their own request to waive “%s”, "+
			"which this installation allows only while nobody else administers the organization. Reason: %s",
			decision.Decider, stepOf(row), decision.Reason)
	}
	entry := approvalNote(requestEntry(eventType, request, row, narrative), request, decision)
	if decision.SelfApproved {
		entry.Data[auditOtherAdministrators] = 0
	}
	return entry
}

// writeEntries writes trail entries about one ledger row, each pointing at
// it, in the caller's unit of work. An entry with no id is given one. A
// wiring with no audit writer (tests only) writes none, as record writes
// none: the ledger row is then the whole record.
func (s *instanceDeviationService) writeEntries(ctx context.Context, row entities.Deviation, entries ...entities.AuditEntry) error {
	if s.actions.audit == nil {
		return nil
	}
	for _, entry := range entries {
		if entry.ID == uuid.Nil {
			id, err := uuid.NewV7()
			if err != nil {
				return err
			}
			entry.ID = id
		}
		entry.Data[auditDeviationID] = row.ID.String()
		if err := s.actions.audit.RecordEvent(ctx, entry); err != nil {
			return fmt.Errorf("writing the %s entry of deviation %s: %w", entry.Type, row.ID, err)
		}
	}
	return nil
}

// expiredAtApproval records that a request reached its deadline with nobody
// having decided it (expireWaive), and refuses the approval that found it so.
// The record is kept: the refusal is given after the commit.
func (s *instanceDeviationService) expiredAtApproval(ctx context.Context, request entities.DeviationRequest) error {
	if err := s.expireWaive(ctx, request); err != nil {
		return err
	}
	return refuseAfterCommit(apierr.Invalidf(
		"This request expired on %s before anybody approved it, so nothing was applied. Ask again if it is still needed.",
		request.ExpiresAt.UTC().Format(decidedOnLayout)))
}

// staleAtApproval records that a request no longer held when somebody came to
// approve it, and refuses the approval. The answer is the one an apply made
// for work that has changed gets — preview again — given to the approver, and
// kept: the refusal is given after the commit.
//
// why is the reason in a few words, and refusals what the plan made at
// approval refused, if it refused anything; the request keeps both as what
// became of it.
func (s *instanceDeviationService) staleAtApproval(
	ctx context.Context,
	request entities.DeviationRequest,
	decision entities.DeviationDecision,
	why string,
	refusals []string,
) error {
	narrative := func(step string) string {
		return fmt.Sprintf("The request to waive “%s” that %s made no longer held when %s tried to approve it, so nothing changed: %s",
			step, request.RequestedBy, decision.Decider, why)
	}
	_, err := s.settleWaiveRequest(ctx, request, entities.DeviationStale, entities.DeviationRequestStale,
		EventDeviationStale, narrative, decision, map[string]any{"why": why, "refusals": listed(refusals)})
	if err != nil {
		return err
	}
	return refuseAfterCommit(apierr.Invalidf("This request no longer holds — %s — so nothing was applied. Preview again and ask afresh.", why))
}

// settleWaiveRequest ends a request for a waive that will not be applied —
// stale, expired or rejected — and its ledger row with it, says so on the
// trail, and answers the request as it then is. Nothing about the instance
// changes, and no instance is locked: its caller holds the request's row, and
// that is the only lock this needs before the ledger row's own.
//
// The request is moved first: the repository refuses the move unless the
// request still waits (ErrDeviationRequestDecided, returned as it is for the
// caller to tell), and nothing else has been written by then. Then the row
// that waited on it, which lets go of its visit — the same waive can be asked
// for again — and names nobody as having approved. Then the entry, when there
// is a trail to write to.
//
// narrative makes the entry's sentence from the step's name, which is read
// off the ledger row: the row is in hand here, and a request read by the
// sweep carries no plan to take a name from. So a request is needed only for
// its id, its instance, the visit it holds, who asked and its deadline.
//
// decision is who rejected the request, or who found it stale; it is zero for
// an expiry, which nobody made. Only a rejection names its decider on the
// request: an expiry and a stale request are the clock's and the instance's
// doing, and say so by naming nobody. A stale request and a rejected one are
// dated by when that happened. An expired one is dated by its deadline, on
// the ledger row as on the request, whenever it was found: the clock decided
// it then, and whoever writes it down only records that.
func (s *instanceDeviationService) settleWaiveRequest(
	ctx context.Context,
	request entities.DeviationRequest,
	rowStatus entities.DeviationStatus,
	requestStatus entities.DeviationRequestStatus,
	eventType string,
	narrative func(step string) string,
	decision entities.DeviationDecision,
	outcome map[string]any,
) (entities.DeviationRequest, error) {
	var none entities.DeviationRequest
	if s.repo.DeviationRequest() == nil || s.repo.DeviationDecider() == nil {
		return none, errNoDeviationRequests
	}
	at := decision.At
	if at.IsZero() {
		at = time.Now()
	}
	change := repocontracts.DeviationRequestChange{Status: requestStatus, Outcome: outcome}
	switch requestStatus {
	case entities.DeviationRequestRejected:
		change.DecidedBy, change.DecidedByID, change.DecisionReason, change.DecidedAt = decision.Decider, decision.DeciderID, decision.Reason, at
	case entities.DeviationRequestStale:
		change.DecidedAt = at
	case entities.DeviationRequestExpired:
		at = request.ExpiresAt
	}
	settled, err := s.repo.DeviationRequest().Transition(ctx, request.ID, entities.DeviationRequestPending, change)
	if err != nil {
		return none, fmt.Errorf("closing request %s as %s: %w", request.ID, requestStatus, err)
	}
	pending, err := s.rowWaitingOn(ctx, request)
	if err != nil {
		return none, err
	}
	row, err := s.repo.DeviationDecider().Decide(ctx, pending.ID, repocontracts.LedgerRowDecision{Status: rowStatus, DecidedAt: at})
	if err != nil {
		return none, fmt.Errorf("closing the ledger row of request %s as %s: %w", request.ID, rowStatus, err)
	}
	entry := requestEntry(eventType, settled, row, narrative(stepOf(row)))
	switch requestStatus {
	case entities.DeviationRequestRejected:
		entry.Data["rejected_by"] = decision.Decider
		// Said only when it is so, as every mark on an entry is.
		if decision.DeciderID == request.RequestedByID {
			entry.Data["withdrawn"] = true
		}
	case entities.DeviationRequestStale:
		entry.Data["attempted_by"] = decision.Decider
	}
	if err := s.writeEntries(ctx, row, entry); err != nil {
		return none, err
	}
	return settled, nil
}
