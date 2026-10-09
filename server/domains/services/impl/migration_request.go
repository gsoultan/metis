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
		Reason:            migrationReason(options),
		Command:           migrationCommandDocument(sourceDefID, targetDefID, nodeMapping, options),
		Plan:              shown,
		Fingerprint:       migrationFingerprint(sourceDefID, targetDefID, nodeMapping, options, plan.ComplianceHolds),
		ApprovedInstances: activeInstanceIDs(covered),
		ExpiresAt:         time.Now().Add(ttl),
	}, nil
}

// migrationReason is what a request for a migration says of why, for whoever
// reads the queue: the reasons its decisions gave, in the order of their
// steps, or — for one that decides nothing and only accepts the loss of a
// control — which controls.
func migrationReason(options servicecontracts.MigrationOptions) string {
	reasons := make([]string, 0, len(options.Actions))
	for _, nodeID := range sortedKeys(options.Actions) {
		if reason := strings.TrimSpace(options.Actions[nodeID].Reason); reason != "" {
			reasons = append(reasons, reason)
		}
	}
	if len(reasons) > 0 {
		return strings.Join(reasons, "; ")
	}
	names := fingerprintNames(options.Acknowledged)
	return "acknowledged the loss of " + strings.Join(names, ", ")
}

// ask writes the request, or answers the one that already holds this
// migration, in one unit of work.
//
// No lock orders two asks made at the same instant. The database does: a
// project has one live request for a fingerprint, the second insert is
// refused, and whoever sent it is told so and answered with the request when
// they send it again.
//
// A failure to read or write is the server's, whatever class it came with:
// the repository answers "not found" for a project it cannot see, and the
// caller has just planned a migration of that project's version.
func (s *migrationService) ask(ctx context.Context, asking entities.DeviationRequest) (entities.PendingApproval, error) {
	var answer entities.PendingApproval
	requests := s.repo.DeviationRequest()
	err := s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		waiting, found, err := requests.FindLive(txCtx, asking.Project.ID, asking.Fingerprint)
		if err != nil {
			return effectFailed("looking for a request that already holds this migration", err)
		}
		if found {
			answer, err = alreadyAsked(waiting, asking, time.Now())
			return err
		}
		asked, err := requests.Create(txCtx, asking)
		if errors.Is(err, repocontracts.ErrDeviationRequestAlreadyWaiting) {
			return apierr.Invalidf("The same migration was asked for at the same moment and is already waiting for approval; " +
				"send this again to be answered with that request.")
		}
		if err != nil {
			return effectFailed("recording the request for this migration", err)
		}
		answer = entities.PendingApprovalOf(asked)
		return nil
	})
	if err != nil {
		return entities.PendingApproval{}, err
	}
	return answer, nil
}

// alreadyAsked is what an ask is told when a request already holds the
// migration: the request itself, to whoever made it; a refusal naming it, to
// anybody else; and, of one that has been approved, that it is being applied.
//
// A state the caller met, not a mistake: there is no class for a conflict, so
// it is an invalid argument, as "already waived by …" is.
func alreadyAsked(waiting, asking entities.DeviationRequest, now time.Time) (entities.PendingApproval, error) {
	var none entities.PendingApproval
	switch waiting.EffectiveStatus(now) {
	case entities.DeviationRequestPending:
		if waiting.RequestedByID != uuid.Nil && waiting.RequestedByID == asking.RequestedByID {
			return entities.PendingApprovalOf(waiting), nil
		}
		return none, apierr.Invalidf("The same migration is already waiting for approval (request %s, asked by %s); approve or reject that one.",
			waiting.ID, waiting.RequestedBy)
	case entities.DeviationRequestApproved:
		return none, apierr.Invalidf("The same migration was approved by %s and is being applied now (request %s); nothing new was asked for.",
			waiting.DecidedBy, waiting.ID)
	}
	return none, apierr.Invalidf("The same migration has a request that is past its time and not yet closed (request %s); ask again shortly.", waiting.ID)
}
