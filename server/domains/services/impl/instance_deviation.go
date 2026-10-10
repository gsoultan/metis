package impl

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
)

// instanceDeviationService deals with one running instance where it stands:
// it waives the step the instance waits at, cancels the instance, or holds it
// — without a second version of its process to migrate it to.
//
// It decides whether to act; what each act does is nodeActions', which a
// migration deciding the same things calls too.
type instanceDeviationService struct {
	repo repositories.Repository
	// engine reads the instance and the graph it runs. nil only in a wiring
	// with no engine, where every command is refused as the server's fault.
	engine servicecontracts.ExecutionEngine
	// actions is what a waive, a cancel and a hold do, and knows whether the
	// engine can end a step whole.
	actions nodeActions
	// rules says who may approve a waive that waits for a second
	// administrator.
	rules approvalRules
}

// NewInstanceDeviationService builds the in-place command over a repository
// and the engine that runs the instances it acts on.
func NewInstanceDeviationService(repo repositories.Repository, engine servicecontracts.ExecutionEngine) servicecontracts.InstanceDeviator {
	return newInstanceDeviationService(repo, engine)
}

// newInstanceDeviationService is NewInstanceDeviationService for what is
// built in this package and needs more of the service than the command: the
// request service approves a waive through it.
func newInstanceDeviationService(repo repositories.Repository, engine servicecontracts.ExecutionEngine) *instanceDeviationService {
	return &instanceDeviationService{repo: repo, engine: engine, actions: newNodeActions(repo, engine), rules: approvalRules{repo: repo}}
}

// errDeviationWithoutEngine is a plain error, not one the caller can fix: it
// is how the server was put together.
var errDeviationWithoutEngine = errors.New(
	"an instance cannot be waived, cancelled or held: this server was wired without the execution engine")

// DeviateInstance answers the plan for a command, and makes the change when
// the command is not a dry run.
//
// The caller is asked for first, then the command is checked, and only then
// is anything read: somebody who may not ask learns nothing about the
// instance, and an administrator of another organization is told there is no
// such instance, as of one that never existed (the repository reads inside
// the organization the request is for).
//
// A dry run reads and does nothing else. It opens no transaction, takes no
// lock and writes nothing, so a preview never makes an instance wait and can
// be asked for as often as anybody likes; what it read may be stale a moment
// later, which is what the visit key in the plan is for.
//
// An apply is one unit of work (apply): everything it changes is kept
// together or not at all. Nothing about the instance is read before that unit
// of work opens — the command is what the caller sent, and the actor is who
// is signed in.
//
// One apply may be made twice. A waive whose visit is held by a request that
// is past its deadline — or that somebody decided while it was being read —
// is not refused and is not left for the sweep to free: the apply ends with
// which request that is (overdueRequest), having written nothing; its unit of
// work has then ended and let go of the instance; the request is closed under
// its own row (closeOverdue); and the apply is made once more, from the
// start. At most once more: a second such answer means the ledger and the
// request disagree, which is the server's to explain. The two units of work
// run one after the other and never one inside the other, so a request's row
// is still never taken while an instance is held.
func (s *instanceDeviationService) DeviateInstance(ctx context.Context, command entities.DeviationCommand) (entities.DeviationOutcome, error) {
	actor, err := requireDeviationAdministrator(ctx)
	if err != nil {
		return entities.DeviationOutcome{}, err
	}
	command, err = normalizedDeviationCommand(command)
	if err != nil {
		return entities.DeviationOutcome{}, err
	}
	if s.engine == nil {
		return entities.DeviationOutcome{}, errDeviationWithoutEngine
	}
	if command.DryRun {
		return s.preview(ctx, command)
	}
	outcome, err := s.applyInItsOwnUnit(ctx, command, actor)
	var overdue overdueRequest
	if !errors.As(err, &overdue) {
		return outcome, err
	}
	if err := s.closeOverdue(ctx, overdue.id); err != nil {
		return entities.DeviationOutcome{}, err
	}
	outcome, err = s.applyInItsOwnUnit(ctx, command, actor)
	if errors.As(err, &overdue) {
		return entities.DeviationOutcome{}, fmt.Errorf("request %s is over, and the ledger still holds a row that waits on it", overdue.id)
	}
	return outcome, err
}

// applyInItsOwnUnit makes one apply in one unit of work, and answers nothing
// of it when it failed.
func (s *instanceDeviationService) applyInItsOwnUnit(ctx context.Context, command entities.DeviationCommand, actor string) (entities.DeviationOutcome, error) {
	var outcome entities.DeviationOutcome
	err := s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		var applyErr error
		outcome, applyErr = s.apply(txCtx, command, actor)
		return applyErr
	})
	if err != nil {
		return entities.DeviationOutcome{}, err
	}
	return outcome, nil
}

// preview answers the plan for the instance as it is read now, without a
// lock, and changes nothing. A waive's says as well when a request already
// waits for the visit (withWhatWaits): that is found by an apply under the
// instance's lock, and nothing that stops an apply is left to be found only
// by making one.
func (s *instanceDeviationService) preview(ctx context.Context, command entities.DeviationCommand) (entities.DeviationOutcome, error) {
	instance, err := s.engine.GetInstance(ctx, command.InstanceID)
	if err != nil {
		return entities.DeviationOutcome{}, err
	}
	plan, err := s.plan(ctx, instance, command)
	if err != nil {
		return entities.DeviationOutcome{}, err
	}
	plan, err = s.withWhatWaits(ctx, plan, command)
	if err != nil {
		return entities.DeviationOutcome{}, err
	}
	return entities.DeviationOutcome{Plan: plan}, nil
}

// apply makes the change a command asks for and answers its record. It runs
// in the unit of work its caller opened, and its order is what it guarantees:
//
//  1. The instance's row is taken, once, and held to the end. Everything
//     below is asked of that row and of nothing read before it, and the act
//     is handed that same row. Two applies of one preview take turns here. So
//     do an apply and a completion of the step, which takes the instance
//     before its task, as this does: whichever is second finds what the first
//     left.
//  2. A request already made is answered with its record (replay), whatever
//     the instance has become since. That is what makes a lost answer safe to
//     ask for again, and why it comes before every refusal.
//  3. The instance is still running (refuseEnded).
//  4. The plan is made again, from the locked row, and is the plan that was
//     previewed, with nothing refusing it; and the instance still waits where
//     the command acts (refuseUnlessAsPreviewed).
//  5. What passed is carried out (carryOut): a waive is recorded as asked
//     for, and waits for a second administrator; a cancel and a hold are
//     acted on.
//
// An instance of another organization is not found by the lock, as it is not
// by a preview's read.
func (s *instanceDeviationService) apply(ctx context.Context, command entities.DeviationCommand, actor string) (entities.DeviationOutcome, error) {
	var none entities.DeviationOutcome
	locked, err := s.repo.Process().GetForUpdate(ctx, command.InstanceID)
	if err != nil {
		return none, err
	}
	if replayed, found, err := s.replay(ctx, command); err != nil || found {
		return replayed, err
	}
	if err := refuseEnded(locked, command.Kind); err != nil {
		return none, err
	}
	// The row as the engine reads it — the entity the engine's own locking
	// read answers — made here from the row this locked, so that the act is
	// handed one row in both shapes and nothing read at another time.
	live := adapters.InstanceEntityAdapter{Model: locked}.ToEntity()
	plan, err := s.plan(ctx, live, command)
	if err != nil {
		return none, err
	}
	if err := refuseUnlessAsPreviewed(locked, plan, command); err != nil {
		return none, err
	}
	return s.carryOut(ctx, locked, &live, plan, command, actor)
}

// needsSecondAdministrator reports whether what a command asks for is not
// one administrator's to do.
//
// A waive never is, whatever its plan says: the plan's flag is what a person
// is shown, and the planner sets it — but were it one day not to, a waive
// would otherwise be made on the word of whoever asked. So the kind decides,
// and the flag can only add to it.
func needsSecondAdministrator(command entities.DeviationCommand, plan entities.DeviationPlan) bool {
	return command.Kind == entities.DeviationWaive || plan.RequiresSecondApprover
}

// carryOut does what an apply was let through to do, on the row it locked.
//
// What needs a second administrator is not acted on: it is recorded as asked
// for (request), and waits. Whoever asks is the account a second
// administrator will have to differ from, so an administrator with no account
// id is refused here. Only a waive has a request to be made for it; anything
// else that a plan said needed somebody else is refused as the server's
// mistake rather than acted on or asked for as though it were a waive.
//
// Everything else is the act (act), which writes the change, its ledger row
// and its trail entry together — and has no waive in it.
func (s *instanceDeviationService) carryOut(
	ctx context.Context,
	locked models.ProcessInstanceModel,
	live *entities.ProcessInstance,
	plan entities.DeviationPlan,
	command entities.DeviationCommand,
	actor string,
) (entities.DeviationOutcome, error) {
	var none entities.DeviationOutcome
	if needsSecondAdministrator(command, plan) {
		if command.Kind != entities.DeviationWaive {
			return none, fmt.Errorf("a %s was planned as needing a second administrator, and only a waive has a request to make for one", command.Kind)
		}
		account, err := requireDecidingAdministrator(ctx)
		if err != nil {
			return none, err
		}
		return s.request(ctx, locked, plan, command, account)
	}
	def, err := s.graphRunBy(ctx, live.ID, uuid.UUID(locked.DefinitionID))
	if err != nil {
		return none, err
	}
	deviation, err := s.act(ctx, locked, live, def, plan, command, actor)
	if err != nil {
		return none, err
	}
	return entities.DeviationOutcome{Plan: plan, Applied: true, Deviation: &deviation}, nil
}
