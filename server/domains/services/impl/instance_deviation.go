package impl

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
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
	var outcome entities.DeviationOutcome
	err = s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
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
// lock, and changes nothing.
func (s *instanceDeviationService) preview(ctx context.Context, command entities.DeviationCommand) (entities.DeviationOutcome, error) {
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
//  5. The act (act), which writes the change, its ledger row and its trail
//     entry together.
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
	def, err := s.graphRunBy(ctx, live.ID, uuid.UUID(locked.DefinitionID))
	if err != nil {
		return none, err
	}
	deviation, err := s.act(ctx, locked, &live, def, plan, command, actor)
	if err != nil {
		return none, err
	}
	return entities.DeviationOutcome{Plan: plan, Applied: true, Deviation: &deviation}, nil
}

// errMovedSincePreview is the refusal of an apply made for work that is no
// longer what its preview showed. It is the caller's to fix, by previewing
// again: there is no class for a conflict, so it is an invalid argument, as
// the engine's own "this step has already finished" is.
func errMovedSincePreview() error {
	return apierr.Invalidf("this instance has moved since you previewed it; preview again")
}

// refuseEnded refuses an act on an instance that is no longer running, from
// the row the apply locked.
//
// The plan refuses this too. It is asked here as well, of the row itself and
// before anything else is read, so that it holds however the plan's own
// refusal is one day worded or relaxed.
//
// A suspended instance has not ended, so it is not said that it can no longer
// be acted on. Nor is it told to resume it first: no route, service or handler
// suspends an instance or resumes one, and a refusal does not tell somebody to
// do what the product cannot. It is said what the instance is and that it is
// refused, in one sentence for all three kinds.
func refuseEnded(locked models.ProcessInstanceModel, kind entities.DeviationKind) error {
	switch locked.Status {
	case models.ProcessActive:
		return nil
	case models.ProcessSuspended:
		return apierr.Invalidf("this instance is suspended, and a suspended instance is not waived, cancelled or held in place")
	}
	return apierr.Invalidf("this instance is %s, so it can no longer be %s; preview again", locked.Status, pastTense(kind))
}

// refuseUnlessAsPreviewed refuses an apply unless the plan made from the
// locked row is the one that was previewed and nothing refuses it, and the
// locked row still waits where the command acts.
//
// The key first: when the work has changed, that is what the caller needs to
// hear, whatever else the new plan would say. Then the plan's own refusals,
// which are what a preview of the same request shows. The last question
// repeats what the key covers and what the plan refuses, on purpose: it is
// read off the locked row, and the act is never reached without it.
func refuseUnlessAsPreviewed(locked models.ProcessInstanceModel, plan entities.DeviationPlan, command entities.DeviationCommand) error {
	if plan.VisitKey != command.VisitKey {
		return errMovedSincePreview()
	}
	if !plan.Applicable() {
		return apierr.Invalidf("%s", strings.Join(plan.Refusals, " "))
	}
	if !waitsWhereItActs(locked, command) {
		return errMovedSincePreview()
	}
	return nil
}

// waitsWhereItActs asks the locked row where the instance stands: on the step
// the command names, or — for a cancel that names none — on no step at all.
//
// On the step means holding a token there, one or several: the question a
// migration's skip asks of the row it locked (parkedOn), put to the row as
// the store holds it. Whether the instance is still running is refuseEnded's.
func waitsWhereItActs(locked models.ProcessInstanceModel, command entities.DeviationCommand) bool {
	if command.NodeID == "" {
		return len(locked.Tokens) == 0
	}
	return holdsWork(locked, command.NodeID)
}

// replay answers a request that was already made with the record of what it
// did, and reports whether it found one.
//
// The visit is looked for by the key the request names: a retry repeats the
// key of the plan it previewed, and the row the first attempt wrote carries
// it. Found, and asking for the same thing, it is answered as done and
// nothing is done again. Found, and asking for something else, it is refused
// with who acted: the visit has had its act.
//
// Its caller holds the instance's lock, so an apply that was in flight for
// the same visit has finished, and its row is here to find.
func (s *instanceDeviationService) replay(ctx context.Context, command entities.DeviationCommand) (entities.DeviationOutcome, bool, error) {
	row, found, err := s.repo.Deviation().FindLiveByVisit(ctx, command.InstanceID, command.VisitKey)
	if err != nil || !found {
		return entities.DeviationOutcome{}, false, err
	}
	same, err := sameRequest(row, command)
	if err != nil {
		return entities.DeviationOutcome{}, false, err
	}
	if !same {
		return entities.DeviationOutcome{}, false, alreadyActedOn(row)
	}
	plan := entities.DeviationPlan{
		InstanceID: command.InstanceID, Kind: row.Kind, Scope: row.Scope, NodeID: command.NodeID, VisitKey: row.VisitKey,
	}
	if row.Node != nil {
		plan.NodeName = cmp.Or(row.Node.Name, row.Node.ID)
	}
	return entities.DeviationOutcome{
		Plan: plan, Applied: row.Status == entities.DeviationApplied, Replayed: true, Deviation: &row,
	}, true, nil
}

// alreadyActedOn is the refusal of a request for a visit that has had its
// act, when it is not that act asked for again. It says what was done and by
// whom, which is what the caller needs to stop asking.
func alreadyActedOn(row entities.Deviation) error {
	what := "this instance"
	if row.Scope == entities.DeviationScopeTask {
		what = "this step"
	}
	return apierr.Invalidf("%s was already %s by %s", what, pastTense(row.Kind), row.Actor)
}

// sameRequest reports whether a command asks for what a row records: the
// same act, on the same step or on none, for the same reason, counting as the
// same values.
func sameRequest(row entities.Deviation, command entities.DeviationCommand) (bool, error) {
	recordedNode := ""
	if row.Node != nil {
		recordedNode = row.Node.ID
	}
	if row.Kind != command.Kind || recordedNode != command.NodeID ||
		strings.TrimSpace(row.Reason) != strings.TrimSpace(command.Reason) {
		return false, nil
	}
	var recorded map[string]any
	if set, has := row.After["variables"]; has {
		values, ok := set.(map[string]any)
		if !ok {
			return false, fmt.Errorf("reading what deviation %s set: it is recorded as %T, not as values by name", row.ID, set)
		}
		recorded = values
	}
	was, err := canonicalValues(recorded)
	if err != nil {
		return false, fmt.Errorf("reading what deviation %s set: %w", row.ID, err)
	}
	asked, err := canonicalValues(command.Outputs)
	if err != nil {
		return false, apierr.Invalidf("the outputs cannot be written as JSON: %v", err)
	}
	return was == asked, nil
}

// canonicalValues writes values the one way two sets of them are compared:
// as JSON, read back and written again. Written once, the keys are in order;
// read back, a number is the number the record keeps, whatever type carried
// it here — 1, 1.0 and a decoder's "1" are one value, as they are once
// stored. No values and an empty set are the same nothing.
func canonicalValues(values map[string]any) (string, error) {
	if len(values) == 0 {
		return "{}", nil
	}
	written, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	var read any
	if err := json.Unmarshal(written, &read); err != nil {
		return "", err
	}
	again, err := json.Marshal(read)
	if err != nil {
		return "", err
	}
	return string(again), nil
}
