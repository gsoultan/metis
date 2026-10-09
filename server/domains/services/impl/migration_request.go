package impl

import (
	"cmp"
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
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
)

// errAskInsideTransaction is what asking for a migration answers when it is
// started inside a transaction somebody else opened. A plain error: it is how
// the code that called it was written.
var errAskInsideTransaction = errors.New(
	"a migration is asked for in a transaction of its own: inside another, the request would be undone with it, " +
		"and a request the clock has closed could not be written down as closed before a fresh one is made")

// RequestMigrationApproval records a migration that skips a step, or drops a
// control instances have not passed, as a request that waits for a second
// administrator. It moves nothing: no instance is read under a lock, and
// nothing but the request is written.
//
// Who is asking is read from whoever is signed in — an administrator of the
// organization, with an account — before anything is planned, and is who the
// request and every record of the run it leads to name as having asked,
// whatever actor the options carried.
//
// The plan is the planner's, made as a dry run makes it. One it refuses is
// refused here in the same words, and one that needs nobody else is not
// asked for: it is applied.
//
// The request stores what was asked (the command the run will be made from),
// what the requester was shown (the plan, and why it needs somebody else),
// the policy's fingerprint, and the instances still running that the plan
// listed — the only instances a run under this request may act on. That list
// is as long as the version has running instances, or as the caller's own
// narrowing: what bounds it is what bounds the plan, which read every one of
// them. Nothing that lists requests reads it.
//
// Asked again for the same migration by whoever asked, it answers the request
// that waits. Asked by anybody else, it refuses and names that request: a
// second administrator does not get a request of their own to approve.
func (s *migrationService) RequestMigrationApproval(
	ctx context.Context,
	sourceDefID, targetDefID uuid.UUID,
	nodeMapping map[string]string,
	opts ...servicecontracts.MigrationOption,
) (entities.PendingApproval, error) {
	var none entities.PendingApproval
	account, err := requireDecidingAdministrator(ctx)
	if err != nil {
		return none, err
	}
	if db.InTransaction(ctx) {
		return none, errAskInsideTransaction
	}
	if s.repo.DeviationRequest() == nil {
		return none, errNoDeviationRequests
	}
	options := servicecontracts.ApplyMigrationOptions(opts)
	options.Actor, options.Approval = account.Username, servicecontracts.MigrationApproval{}
	plan, covered, err := s.planFor(ctx, sourceDefID, targetDefID, nodeMapping, options)
	if err != nil {
		return none, err
	}
	if !plan.Applicable() {
		return none, apierr.Invalidf("%s", strings.Join(plan.Refusals, "; "))
	}
	if !plan.RequiresSecondApprover {
		return none, apierr.Invalidf("This migration needs no second administrator; apply it.")
	}
	asking, err := s.migrationRequest(ctx, account, sourceDefID, targetDefID, nodeMapping, options, plan, covered)
	if err != nil {
		return none, err
	}
	return s.ask(ctx, asking)
}

// migrationRequest is the request for a migration as it is first written. Its
// project is the source version's — the planner has refused a migration
// between projects — and its deadline is fixed now, from the setting as it is
// now.
func (s *migrationService) migrationRequest(
	ctx context.Context,
	account entities.User,
	sourceDefID, targetDefID uuid.UUID,
	nodeMapping map[string]string,
	options servicecontracts.MigrationOptions,
	plan entities.MigrationPlan,
	covered []models.ProcessInstanceModel,
) (entities.DeviationRequest, error) {
	source, err := s.repo.Definition().Get(ctx, sourceDefID)
	if err != nil {
		return entities.DeviationRequest{}, fmt.Errorf("source definition: %w", err)
	}
	target, err := s.repo.Definition().Get(ctx, targetDefID)
	if err != nil {
		return entities.DeviationRequest{}, fmt.Errorf("target definition: %w", err)
	}
	shown, err := migrationPlanDocument(plan, plan.SecondApproverReasons)
	if err != nil {
		return entities.DeviationRequest{}, err
	}
	// Its problem, if it has one, was said when the server started.
	ttl, _ := DeviationApprovalTTL()
	return entities.DeviationRequest{
		Project:           &entities.Project{ID: uuid.UUID(source.ProjectID)},
		Kind:              entities.DeviationRequestMigration,
		Status:            entities.DeviationRequestPending,
		SourceDefinition:  &entities.ProcessDefinition{ID: sourceDefID},
		TargetDefinition:  &entities.ProcessDefinition{ID: targetDefID},
		RequestedBy:       account.Username,
		RequestedByID:     account.ID,
		Reason:            migrationReason(options, redirectedSteps(nodeIndex(source.Nodes), nodeIndex(target.Nodes), nodeMapping)),
		Command:           migrationCommandDocument(sourceDefID, targetDefID, nodeMapping, options),
		Plan:              shown,
		Fingerprint:       migrationFingerprint(sourceDefID, targetDefID, nodeMapping, options, plan.ComplianceHolds),
		ApprovedInstances: activeInstanceIDs(covered),
		ExpiresAt:         time.Now().Add(ttl),
	}, nil
}

// migrationReason is what a request for a migration says of why, for whoever
// reads the queue: the reasons its decisions gave, in the order of their
// steps; then which controls' loss was acknowledged; then which steps the
// mapping redirects. Each part is there only when the migration has it, so a
// request that only skips a step reads as the reason somebody typed.
//
// A request always says something: one that decides nothing and acknowledges
// nothing waits because of what its mapping redirects, and says that.
func migrationReason(options servicecontracts.MigrationOptions, redirected []string) string {
	var parts []string
	for _, nodeID := range sortedKeys(options.Actions) {
		if reason := strings.TrimSpace(options.Actions[nodeID].Reason); reason != "" {
			parts = append(parts, reason)
		}
	}
	if acknowledged := fingerprintNames(options.Acknowledged); len(acknowledged) > 0 {
		parts = append(parts, "acknowledged the loss of "+strings.Join(acknowledged, ", "))
	}
	if len(redirected) > 0 {
		parts = append(parts, "redirected "+strings.Join(redirected, ", "))
	}
	return strings.Join(parts, "; ")
}

// redirectedSteps is the steps a mapping sends to a different step, each as
// "“from” to “to”" by name, in the order of the ids they are sent from. A
// rename is not one (renamedSteps).
func redirectedSteps(sourceNodes, targetNodes map[string]models.FlowNode, nodeMapping map[string]string) []string {
	renames := renamedSteps(sourceNodes, nodeMapping)
	var redirected []string
	for _, from := range sortedKeys(nodeMapping) {
		to := nodeMapping[from]
		if _, renamed := renames[from]; renamed || to == from {
			continue
		}
		redirected = append(redirected, fmt.Sprintf("“%s” to “%s”", cmp.Or(sourceNodes[from].Name, from), cmp.Or(targetNodes[to].Name, to)))
	}
	return redirected
}

// errAskedAtTheSameMoment is how one attempt to ask says that the database
// refused its request because another, for the same migration, was written
// at the same instant. It never reaches a caller: ask reads again.
var errAskedAtTheSameMoment = errors.New("the same migration was asked for at the same moment")

// ask writes the request, or answers the one that already holds this
// migration — closing first, when it has to, a request the clock has closed
// and nobody has written down as closed.
//
// At most two attempts (askOnce), each a unit of work of its own, with the
// closing — another unit of work of its own — between them. Never nested:
// the first attempt has ended, having written nothing, before the closing
// takes the old request's row, so a request's row is never taken inside
// another transaction.
//
// The one retry serves two cases. A live request the clock has closed — past
// its deadline, or approved with its run window gone — is closed as the sweep
// would close it (closeWhatTheClockClosed), and the migration asked for
// afresh: it does not hold its migration until the sweep comes round. And two
// asks at the same instant, which no lock orders: the database lets one
// request in, the other reads again and finds it.
//
// A second attempt that fares no better is refused in plain words. That takes
// somebody else closing, or asking, in the instant between — and asking once
// more answers it.
func (s *migrationService) ask(ctx context.Context, asking entities.DeviationRequest) (entities.PendingApproval, error) {
	var none entities.PendingApproval
	answer, closed, err := s.askOnce(ctx, asking)
	if closed == uuid.Nil && !errors.Is(err, errAskedAtTheSameMoment) {
		return answer, err
	}
	if closed != uuid.Nil {
		if err := s.closeWhatTheClockClosed(ctx, closed); err != nil {
			return none, err
		}
	}
	answer, closed, err = s.askOnce(ctx, asking)
	switch {
	case closed != uuid.Nil:
		return none, apierr.Invalidf("The same migration has a request that is past its time and could not be closed just now (request %s); ask again.", closed)
	case errors.Is(err, errAskedAtTheSameMoment):
		return none, apierr.Invalidf("The same migration was asked for at the same moment and is already waiting for approval; " +
			"send this again to be answered with that request.")
	}
	return answer, err
}

// askOnce is one attempt to ask, in one unit of work: it writes the request,
// or answers the live request that already holds the migration, or says
// which live request the clock has closed (closedByTheClock) — having
// written nothing.
//
// A failure to read or write is the server's, whatever class it came with:
// the repository answers "not found" for a project it cannot see, and the
// caller has just planned a migration of that project's version.
func (s *migrationService) askOnce(ctx context.Context, asking entities.DeviationRequest) (answer entities.PendingApproval, closedByTheClock uuid.UUID, err error) {
	requests := s.repo.DeviationRequest()
	err = s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		waiting, found, err := requests.FindLive(txCtx, asking.Project.ID, asking.Fingerprint)
		if err != nil {
			return effectFailed("looking for a request that already holds this migration", err)
		}
		if found {
			answer, closedByTheClock, err = alreadyAsked(waiting, asking, time.Now())
			return err
		}
		asked, err := requests.Create(txCtx, asking)
		if errors.Is(err, repocontracts.ErrDeviationRequestAlreadyWaiting) {
			return errAskedAtTheSameMoment
		}
		if err != nil {
			return effectFailed("recording the request for this migration", err)
		}
		answer = entities.PendingApprovalOf(asked)
		return nil
	})
	if err != nil {
		return entities.PendingApproval{}, uuid.Nil, err
	}
	return answer, closedByTheClock, nil
}

// alreadyAsked is what an ask is told when a live request already holds the
// migration: the request itself, to whoever made it; a refusal naming it, to
// anybody else; of one that has been approved and may still be running, that
// it is being applied; and of one the clock has closed — past its deadline,
// or approved with its run window gone — its id, for the ask to close it and
// ask afresh.
//
// The request is as the table holds it, so it is read through the clock
// (EffectiveStatus): what is stored says waiting, or approved, long after
// either stopped being true.
//
// A refusal is a state the caller met, not a mistake: there is no class for a
// conflict, so it is an invalid argument, as "already waived by …" is.
func alreadyAsked(waiting, asking entities.DeviationRequest, now time.Time) (entities.PendingApproval, uuid.UUID, error) {
	var none entities.PendingApproval
	switch waiting.EffectiveStatus(now) {
	case entities.DeviationRequestPending:
		if waiting.RequestedByID != uuid.Nil && waiting.RequestedByID == asking.RequestedByID {
			return entities.PendingApprovalOf(waiting), uuid.Nil, nil
		}
		return none, uuid.Nil, apierr.Invalidf("The same migration is already waiting for approval (request %s, asked by %s); approve or reject that one.",
			waiting.ID, waiting.RequestedBy)
	case entities.DeviationRequestApproved:
		return none, uuid.Nil, apierr.Invalidf("The same migration was approved by %s and is being applied now (request %s); nothing new was asked for.",
			waiting.DecidedBy, waiting.ID)
	}
	return none, waiting.ID, nil
}

// closeWhatTheClockClosed writes down that a live request for a migration is
// over — expired, if it waited past its deadline; interrupted, if it was
// approved and its run window closed with no report — so that the migration
// can be asked for again now and not when the sweep next comes round.
//
// It is a unit of work of its own (runDecision), and takes the request's row
// and nothing else. What it writes is what the sweep writes for the request,
// through the same two functions, under the same row: the two can meet, and
// whichever has the row closes the request while the other finds it closed.
//
// The request is read again under its row, without its documents — closing
// it needs none — and judged again against the clock. One that is no longer
// live, or not closed by the clock after all, is left alone: somebody decided
// it meanwhile, and the ask that follows finds what they left.
func (s *migrationService) closeWhatTheClockClosed(ctx context.Context, id uuid.UUID) error {
	requests := s.repo.DeviationRequest()
	return runDecision(ctx, s.repo.UnitOfWork(), func(txCtx context.Context) error {
		request, err := requests.GetForUpdateWithoutDocuments(txCtx, id)
		if err != nil {
			return effectFailed(fmt.Sprintf("reading request %s, which the clock has closed", id), err)
		}
		if request.Kind != entities.DeviationRequestMigration {
			return nil
		}
		now := time.Now()
		switch {
		case request.Status == entities.DeviationRequestPending && request.EffectiveStatus(now) == entities.DeviationRequestExpired:
			err = expireMigrationRequest(txCtx, requests, request)
		case request.Status == entities.DeviationRequestApproved && request.RunWindowClosed(now):
			err = interruptUnreported(txCtx, requests, request)
		}
		if err != nil && !errors.Is(err, repocontracts.ErrDeviationRequestDecided) {
			return effectFailed(fmt.Sprintf("recording that request %s is over", id), err)
		}
		return nil
	})
}
