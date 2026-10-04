package impl

import (
	"context"
	"errors"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
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
}

// NewInstanceDeviationService builds the in-place command over a repository
// and the engine that runs the instances it acts on.
func NewInstanceDeviationService(repo repositories.Repository, engine servicecontracts.ExecutionEngine) servicecontracts.InstanceDeviator {
	return &instanceDeviationService{repo: repo, engine: engine, actions: newNodeActions(repo, engine)}
}

// errDeviationWithoutEngine is a plain error, not one the caller can fix: it
// is how the server was put together.
var errDeviationWithoutEngine = errors.New(
	"an instance cannot be waived, cancelled or held: this server was wired without the execution engine")

// DeviateInstance answers the plan for a command.
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
func (s *instanceDeviationService) DeviateInstance(ctx context.Context, command entities.DeviationCommand) (entities.DeviationOutcome, error) {
	if _, err := requireDeviationAdministrator(ctx); err != nil {
		return entities.DeviationOutcome{}, err
	}
	command, err := normalizedDeviationCommand(command)
	if err != nil {
		return entities.DeviationOutcome{}, err
	}
	if s.engine == nil {
		return entities.DeviationOutcome{}, errDeviationWithoutEngine
	}
	if !command.DryRun {
		return entities.DeviationOutcome{}, apierr.Invalidf("preview with dry_run: applying arrives with the in-place apply")
	}
	instance, err := s.engine.GetInstance(ctx, command.InstanceID)
	if err != nil {
		return entities.DeviationOutcome{}, err
	}
	plan, err := s.plan(ctx, instance, command)
	if err != nil {
		return entities.DeviationOutcome{}, err
	}
	return entities.DeviationOutcome{Plan: plan}, nil
}
