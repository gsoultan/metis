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
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// approvedRun is a migration a second administrator has just approved: the
// request as the approval left it, and the migration it stored, ready to be
// applied under it.
type approvedRun struct {
	request        entities.DeviationRequest
	decision       entities.DeviationDecision
	source, target uuid.UUID
	mapping        map[string]string
	options        []servicecontracts.MigrationOption
	plan           entities.MigrationPlan
}

// approveRequest approves a request for a migration and runs it, in three
// steps, none inside another's transaction.
//
// A — the approval (admit), one unit of work holding the request's row and
// nothing else: who may approve, whether the request still waits and is in
// time, and whether the migration it stored is still the one that was asked
// for, over no instance the request did not show. It ends with the request
// approved and committed — or expired or stale, committed, and refused.
//
// B — the run: ApplyInstanceMigration, called as anybody calls it, with the
// stored migration and the request's id. Its gate plans again and verifies
// the request again against the plan the run is made from, so what was
// approved is what runs or nothing does; an instance that arrived on the
// version between A and B is refused there. The request's row is not held:
// a run takes its instances one at a time, for as long as it takes.
//
// C — the report (reportRun), one unit of work holding the request's row
// again: what the run did, written on the request. Approved is never where a
// request rests.
//
// It answers the request as C left it, what the run did, and the plan the
// approval was made from. When the run failed, its failure comes back beside
// them: the request reads interrupted, and what the run had done stands.
//
// Who is approving is asked here, of whoever is signed in, whatever its
// caller checked first — as the approval of a waive asks it.
func (s *migrationService) approveRequest(ctx context.Context, id uuid.UUID, reason string) (entities.DeviationRequestOutcome, error) {
	var none entities.DeviationRequestOutcome
	caller, err := requireDecidingAdministrator(ctx)
	if err != nil {
		return none, err
	}
	if s.repo.DeviationRequest() == nil {
		return none, errNoDeviationRequests
	}
	run, err := s.admit(ctx, id, caller, reason)
	if err != nil {
		return none, err
	}
	if run.decision.SelfApproved {
		traceSelfApproval(run.request, run.decision.Organization)
	}

	result, reported, runErr := s.runAndReport(ctx, run, id)
	outcome := entities.DeviationRequestOutcome{
		Request: reported,
		// The migrate route's own rule: applied unless the run passed
		// instances over and wrote to none.
		Applied:         runErr == nil && (result.Changed > 0 || len(result.PassedOver) == 0),
		MigrationPlan:   &run.plan,
		MigrationResult: &result,
	}
	if runErr != nil {
		traceApprovedRunStopped(run.request, result, runErr)
		return outcome, approvedRunFailed(id, reported.Status, runErr)
	}
	return outcome, nil
}

// errRunPanicked stands for a run that did not return: it panicked. What it
// had done by then is not known to the report made of it.
var errRunPanicked = errors.New("the run panicked")

// runAndReport is steps B and C: the run, and the report of it on the
// request — which is made whatever became of the run.
//
// A run that panics does not come back to be reported on. Left at that, its
// request would stay approved until its window closed, holding the migration
// against anybody asking again. So the report is owed from a deferred call:
// the request is closed as interrupted, saying the server failed and that how
// far the run got is not known, and the panic then goes on its way — as a
// transaction that panics is rolled back and the panic passed on
// (repositories/db). Nothing here recovers from it.
func (s *migrationService) runAndReport(ctx context.Context, run approvedRun, id uuid.UUID) (result entities.MigrationResult, reported entities.DeviationRequest, runErr error) {
	began := time.Now()
	returned := false
	defer func() {
		if returned {
			return
		}
		recovered := recover()
		s.reportRun(ctx, run.request, result, errRunPanicked, began)
		traceApprovedRunStopped(run.request, result, errRunPanicked)
		if recovered != nil {
			panic(recovered)
		}
	}()
	result, runErr = s.ApplyInstanceMigration(ctx, run.source, run.target, run.mapping,
		append(run.options, servicecontracts.WithApprovedRequest(id))...)
	returned = true
	return result, s.reportRun(ctx, run.request, result, runErr, began), runErr
}

// approvedRunFailure is an approved run that did not finish, as its approver
// is told of it: in words of its own, over the failure that stopped the run.
//
// The words are not the failure's (approvedRunFailed). The failure stays
// under them, so that what it was is still asked of it: a refusal is still a
// refusal to whoever answers the request, and errors.Is and errors.As find
// what they found in the run's own error.
type approvedRunFailure struct {
	said  string
	cause error
}

func (f approvedRunFailure) Error() string { return f.said }

func (f approvedRunFailure) Unwrap() error { return f.cause }

// answeredClasses are the classes a failure is answered by, in the order a
// transport asks after them (common.CodeFrom).
var answeredClasses = []error{apierr.ErrInvalidArgument, apierr.ErrNotFound, apierr.ErrForbidden}

// approvedRunFailed is what whoever approved a migration is told when its run
// did not finish: why, in the failure's words, what became of the request,
// and the one thing there is to do — ask again for what remains.
//
// The failure's own text is a run's, written for a migration one
// administrator applied: it says to run the same migration again, which
// under an approval is not true — the request is spent — so that instruction
// is taken out, and so is the engine's marker for an error a process may
// catch, which is no word.
//
// The failure keeps its class. A run the plan refuses — an instance moved on,
// between the approval and the run, to where the migration cannot take it —
// is a refusal of what was asked, as it is on one administrator's call, and
// the gate's refusal is one of something that may not be done. Neither is the
// server's failure, and neither is counted as one. The class is said once, in
// front, as every classed failure says it; a failure with none is the
// server's, as it was.
func approvedRunFailed(id uuid.UUID, status entities.DeviationRequestStatus, runErr error) error {
	cause := strings.ReplaceAll(withoutClass(runErr), resumeByRunningAgain, "")
	cause = strings.ReplaceAll(cause, "BPMN_ERROR:", "")
	said := fmt.Sprintf("the approved migration did not finish: %s. Request %s now reads %s; what its run had done stands, "+
		"and what remains has to be asked for again", cause, id, status)
	for _, class := range answeredClasses {
		if errors.Is(runErr, class) {
			said = class.Error() + ": " + said
			break
		}
	}
	return approvedRunFailure{said: said, cause: runErr}
}

// admit is step A: the approval, in a unit of work of its own (runDecision),
// holding the request's row and no instance.
//
// A request found past its deadline, or no longer true of the version, is
// recorded as expired or stale and then refused: that record is kept. Any
// other failure undoes everything and the request waits as it did. A second
// decision that a repository refuses as made first is told as the request's
// own state (decidedFirst).
func (s *migrationService) admit(ctx context.Context, id uuid.UUID, caller entities.User, reason string) (approvedRun, error) {
	var run approvedRun
	requests := s.repo.DeviationRequest()
	err := runDecision(ctx, s.repo.UnitOfWork(), func(txCtx context.Context) error {
		var err error
		run, err = s.admitLocked(txCtx, id, caller, reason)
		return decidedFirst(err, func() (entities.DeviationRequest, error) { return requests.GetReadable(txCtx, id) })
	})
	if err != nil {
		return approvedRun{}, err
	}
	return run, nil
}

// admitLocked is admit inside its unit of work.
//
//  1. The request's row is taken and held, whole: what is about to be run is
//     in its documents, and one that no longer opens is not approved.
//  2. Whether it still waits, and who may decide it, are asked of that row
//     (approvalRules.admit) — the rule a waive's approval asks. The requester
//     is refused here, having changed nothing, and the attempt is said in the
//     server's log.
//  3. A request past its deadline is recorded as expired and refused.
//  4. The migration is read from the request's stored command, authorised by
//     whoever asked.
//  5. It is planned again. A plan that can no longer be made, one that now
//     refuses, another policy than the one asked for, or an instance the
//     request did not show makes the request stale: recorded, and refused.
//  6. The request is approved: who, from which account, why and when — and,
//     for an approval by whoever asked, what that exception rested on.
//
// No instance is locked at any step: the plan only reads.
func (s *migrationService) admitLocked(ctx context.Context, id uuid.UUID, caller entities.User, reason string) (approvedRun, error) {
	var none approvedRun
	requests := s.repo.DeviationRequest()
	request, err := requests.GetForUpdate(ctx, id)
	if err != nil {
		return none, err
	}
	if request.Kind != entities.DeviationRequestMigration {
		return none, fmt.Errorf("request %s is a %s, and was handed to the approval of a migration", request.ID, request.Kind)
	}
	decision, err := s.rules.admit(ctx, request, caller, reason)
	if err != nil {
		if caller.ID == request.RequestedByID && errors.Is(err, apierr.ErrForbidden) {
			traceRefusedSelfApproval(request, caller)
		}
		return none, err
	}
	if request.EffectiveStatus(decision.At) == entities.DeviationRequestExpired {
		if err := expireMigrationRequest(ctx, requests, request); err != nil {
			return none, err
		}
		return none, refuseAfterCommit(apierr.Invalidf(
			"This request expired on %s before anybody approved it, so nothing was applied. Ask again if it is still needed.",
			request.ExpiresAt.UTC().Format(decidedOnLayout)))
	}
	run, err := storedRun(request)
	if err != nil {
		return none, err
	}
	run.decision = decision
	why, refusals, err := s.whyStale(ctx, request, &run)
	if err != nil {
		return none, err
	}
	if why != "" {
		return none, staleMigrationRequest(ctx, requests, request, decision, why, refusals)
	}
	change := repocontracts.DeviationRequestChange{
		Status: entities.DeviationRequestApproved, DecidedBy: decision.Decider, DecidedByID: decision.DeciderID,
		DecisionReason: decision.Reason, DecidedAt: decision.At, Outcome: selfApprovalRecord(decision),
	}
	run.request, err = requests.Transition(ctx, request.ID, entities.DeviationRequestPending, change)
	if err != nil {
		return none, fmt.Errorf("recording the approval of request %s: %w", request.ID, err)
	}
	return run, nil
}

// storedRun is the migration a request asks for, read from what the request
// stores, authorised by whoever asked.
//
// A command that cannot be read whole, or that names other versions than its
// request does, is the server's trouble and a plain error — the two were
// written together, by this server — and nothing is approved on it.
func storedRun(request entities.DeviationRequest) (approvedRun, error) {
	var none approvedRun
	stored, err := migrationCommandFrom(request.Command)
	if err != nil {
		return none, fmt.Errorf("request %s cannot be approved: %w", request.ID, err)
	}
	source, target, err := stored.versions()
	if err != nil {
		return none, fmt.Errorf("request %s cannot be approved: %w", request.ID, err)
	}
	if (request.SourceDefinition != nil && request.SourceDefinition.ID != source) ||
		(request.TargetDefinition != nil && request.TargetDefinition.ID != target) {
		return none, fmt.Errorf("request %s cannot be approved: it is not for the versions its command names", request.ID)
	}
	options, err := stored.options(request.RequestedBy)
	if err != nil {
		return none, fmt.Errorf("request %s cannot be approved: %w", request.ID, err)
	}
	return approvedRun{source: source, target: target, mapping: stored.NodeMapping, options: options}, nil
}

// whyStale plans the stored migration again and says why its request no
// longer holds — in a few words, with what the plan refused if it refused —
// or says nothing when it does. The plan made is kept in run.
//
// A plan that cannot be made because of what it is asked to do — a version
// that is gone, an instance named that no longer runs on it — makes the
// request stale: asked again, it would be refused the same way. A plan that
// cannot be made because something failed is that failure, and the request
// waits: a read that timed out says nothing about the request.
func (s *migrationService) whyStale(ctx context.Context, request entities.DeviationRequest, run *approvedRun) (string, []string, error) {
	options := servicecontracts.ApplyMigrationOptions(run.options)
	plan, covered, err := s.planFor(ctx, run.source, run.target, run.mapping, options)
	switch {
	case errors.Is(err, apierr.ErrInvalidArgument), errors.Is(err, apierr.ErrNotFound):
		why := "the migration can no longer be planned: " + withoutClass(err)
		return why, []string{why}, nil
	case err != nil:
		return "", nil, fmt.Errorf("planning the migration request %s asks for: %w", request.ID, err)
	case !plan.Applicable():
		return "the plan now refuses it: " + strings.Join(plan.Refusals, "; "), plan.Refusals, nil
	}
	run.plan = plan
	fingerprint := migrationFingerprint(run.source, run.target, run.mapping, options, plan.ComplianceHolds)
	if why := whyNoLongerHolds(request, fingerprint, activeInstanceIDs(covered)); why != "" {
		return why, []string{why}, nil
	}
	return "", nil, nil
}

// withoutClass is an error in its own words, without the class it was
// answered with: "invalid argument: x" is "x".
func withoutClass(err error) string {
	said := err.Error()
	for _, class := range answeredClasses {
		said = strings.ReplaceAll(said, class.Error()+": ", "")
	}
	return said
}

// staleMigrationRequest records that a request for a migration no longer
// held when somebody came to approve it, and refuses the approval. The
// record is kept: the refusal is given after the commit.
//
// The request's own row is the record, as it is of an expiry
// (expireMigrationRequest): nothing was done to any instance. It names
// nobody as having decided — the version made it stale — and is dated by
// when that was found; who found it is in its outcome.
func staleMigrationRequest(
	ctx context.Context,
	requests repocontracts.DeviationRequestRepository,
	request entities.DeviationRequest,
	decision entities.DeviationDecision,
	why string,
	refusals []string,
) error {
	_, err := requests.Transition(ctx, request.ID, entities.DeviationRequestPending, repocontracts.DeviationRequestChange{
		Status: entities.DeviationRequestStale, DecidedAt: decision.At,
		Outcome: map[string]any{"why": why, "refusals": listed(refusals), "attempted_by": decision.Decider},
	})
	if err != nil {
		return fmt.Errorf("closing request %s as %s: %w", request.ID, entities.DeviationRequestStale, err)
	}
	return refuseAfterCommit(apierr.Invalidf("This request no longer holds, so nothing was applied: %s. Ask again.", why))
}
