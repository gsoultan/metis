package impl

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// outcomeWaived is what the trail says became of a step that was waived: the
// word that keeps it from reading as a step somebody performed.
const outcomeWaived = "waived"

// act makes the change a command asks for and records it, and answers the
// ledger row it wrote.
//
// Whether to act has been decided: its caller holds the instance's lock and
// has asked the locked row everything there is to ask. locked and live are
// that one row, as the store holds it and as the engine reads it; def is the
// graph it runs and plan the plan made from it. The change, the row and the
// trail entry are written in the caller's unit of work, so they are kept
// together or not at all.
func (s *instanceDeviationService) act(
	ctx context.Context,
	locked models.ProcessInstanceModel,
	live *entities.ProcessInstance,
	def *entities.ProcessDefinition,
	plan entities.DeviationPlan,
	command entities.DeviationCommand,
	actor string,
) (entities.Deviation, error) {
	switch command.Kind {
	case entities.DeviationWaive:
		return s.waiveStep(ctx, locked, live, def, plan, command, actor)
	case entities.DeviationCancel:
		return s.cancelWhereItStands(ctx, locked, plan, command, actor)
	case entities.DeviationHold:
		return s.holdWhereItStands(ctx, locked, plan, command, actor)
	}
	return entities.Deviation{}, fmt.Errorf("acting on instance %s: %q is not something done to an instance in place", live.ID, command.Kind)
}

// waiveStep ends the step an instance waits at as waived: what the waiver
// counts as is set, the work open on the step is withdrawn and whoever held
// it told, the step is ended whole and the instance moved on from it once —
// and the ledger and the trail say it was waived, by whom and why.
//
// The values are set before the instance moves on, so what follows the step
// decides from what the waiver counts as and not from what an earlier visit
// left. They are saved with the instance as the advance saves it.
//
// The record is made from the tasks the effect held and withdrew, not from
// the plan's open work: the plan read them without their rows, and whoever
// holds a task may have changed since.
//
// An advance that fails — a gateway after the step with no flow for the
// value given — fails the unit of work: nothing is set, withdrawn or
// recorded. What the caller is told of it is waiveFailed's to say.
func (s *instanceDeviationService) waiveStep(
	ctx context.Context,
	locked models.ProcessInstanceModel,
	live *entities.ProcessInstance,
	def *entities.ProcessDefinition,
	plan entities.DeviationPlan,
	command entities.DeviationCommand,
	actor string,
) (entities.Deviation, error) {
	runID, err := uuid.NewV7()
	if err != nil {
		return entities.Deviation{}, err
	}
	// Read before anything is set: live shares its values with locked.
	held := valuesHeld(live.Variables, command.Outputs)
	for name, value := range command.Outputs {
		live.SetVariable(name, value)
	}
	withdrawn, err := s.actions.waive(ctx, live, def, command.NodeID)
	if err != nil {
		return entities.Deviation{}, waiveFailed(uuid.UUID(locked.ID), callerOf(locked), plan.NodeName, len(command.Outputs) > 0, err)
	}

	row := inPlaceDeviation(locked, plan, command, actor, runID)
	if len(withdrawn) == 1 {
		row.Task = &entities.Task{ID: uuid.UUID(withdrawn[0].ID)}
	}
	row.Before, row.After = withdrawnTaskValues(withdrawn)
	if len(command.Outputs) > 0 {
		row.After["variables"] = maps.Clone(command.Outputs)
		if len(held) > 0 {
			row.Before["variables"] = held
		}
	}
	// Counts, and no business value: the details of a row are not sealed.
	// The decision points are counted and not listed, because a plan lists
	// no more than a screenful of them and a list here would read as all.
	row.Details = map[string]any{"withdrawn": len(withdrawn), "decision_points": plan.DecisionPointsInAll}

	recorded, err := s.actions.record(ctx, row, waiveEntry(locked, plan, command, actor, runID))
	if err != nil {
		return entities.Deviation{}, fmt.Errorf("recording that “%s” was waived: %w", plan.NodeName, err)
	}
	return recorded, nil
}

// waiveFailed is what somebody who asked for a waive is told when the effect
// failed: withdrawing the step's work, or moving the instance on from it.
//
// One failure is a refusal. A gateway reached by the advance found no flow
// for the values the instance then held and declares no default. It is told
// naming the gateway, and it says that nothing was changed: the error is
// returned through the unit of work, which undoes the waive whole. It is a
// refusal — the waive cannot be applied as things stand — whether or not
// another request could be: in this command every such answer is one.
//
// What the caller is told to do about it depends on whose gateway it was and
// on what they gave. With values given and the gateway this instance's, they
// are told to give one a branch accepts. With none given there is nothing of
// theirs that fitted badly, and the form may have no field the gateway reads:
// they are told the instance held no way out. And the advance does not stop
// at the end of this instance — it runs on into the process that called it,
// into one a later step calls, into one a signal wakes — so the gateway may
// be in a definition no preview of this instance showed. Then nobody is told
// to supply a value for it, and it is said to be the caller's only when it
// is: callerID is the instance that started this one, or nil.
//
// Every other failure is the server's (effectFailed), whatever class it came
// with — and so is a gateway's when anything else failed beside it
// (soleGatewayFailure).
func waiveFailed(instanceID, callerID uuid.UUID, step string, gaveOutputs bool, err error) error {
	noFlow := soleGatewayFailure(err)
	if noFlow == nil {
		return effectFailed(fmt.Sprintf("waiving “%s”", step), err)
	}
	gateway := cmp.Or(shownStepName(noFlow.GatewayName), noFlow.GatewayID)
	const unchanged = "so the waive was not applied and nothing was changed."
	switch {
	case noFlow.InstanceID == instanceID && gaveOutputs:
		return apierr.Invalidf("The values given fit no way out of “%s”, %s Preview again and give a value one of its branches accepts.",
			gateway, unchanged)
	case noFlow.InstanceID == instanceID:
		return apierr.Invalidf("“%s” had no way out for the values this instance holds, %s", gateway, unchanged)
	case callerID != uuid.Nil && noFlow.InstanceID == callerID:
		return apierr.Invalidf("“%s”, in the process that started this one, had no way out for the result, %s", gateway, unchanged)
	}
	return apierr.Invalidf("“%s”, in another process this waive reached, had no way out, %s", gateway, unchanged)
}

// soleGatewayFailure answers the gateway with no way out that a failure
// consists of, and nil when the failure is, or holds, anything else.
//
// A failure is a tree. Most of it is a chain — each layer wrapping the one
// below with what it was doing — and such a chain is the gateway's failure
// when its last link is. But a signal or a message delivered to several
// instances collects what failed for each and joins them (BroadcastSignal,
// SendMessage), and one of those may be a gateway while another is the
// database. Told as the gateway's, that would be a refusal for something no
// request can put right, with the server's failure unsaid. So a join is the
// gateway's only when every part of it is; when several are, the first is
// the one named.
//
// This is told from the tree alone, and can be: everything on the way up
// wraps with %w or joins. A layer that kept only the words of what it
// wrapped would hide the gateway, and the failure would be the server's —
// the safe way to be wrong.
func soleGatewayFailure(err error) *entities.NoFlowSelectedError {
	for err != nil {
		// Each link is looked at as itself, not through errors.As: the walk
		// is the unwrapping, and it has to stop at a join to ask of every part.
		if noFlow, is := err.(*entities.NoFlowSelectedError); is { //nolint:errorlint // one link of the walk, by design
			return noFlow
		}
		switch wrapper := err.(type) { //nolint:errorlint // one link of the walk, by design
		case interface{ Unwrap() error }:
			err = wrapper.Unwrap()
		case interface{ Unwrap() []error }:
			return soleGatewayFailureOfAll(wrapper.Unwrap())
		default:
			return nil
		}
	}
	return nil
}

// soleGatewayFailureOfAll is soleGatewayFailure for the parts of a join: the
// first part's gateway when every part is a gateway's failure, and nil when
// any is not or there are none.
func soleGatewayFailureOfAll(parts []error) *entities.NoFlowSelectedError {
	var first *entities.NoFlowSelectedError
	for _, part := range parts {
		if part == nil {
			continue
		}
		noFlow := soleGatewayFailure(part)
		if noFlow == nil {
			return nil
		}
		if first == nil {
			first = noFlow
		}
	}
	return first
}

// callerOf is the instance that started the one a row is of, or nil for one
// nothing started.
func callerOf(locked models.ProcessInstanceModel) uuid.UUID {
	if locked.ParentInstanceID == nil {
		return uuid.Nil
	}
	return uuid.UUID(*locked.ParentInstanceID)
}

// effectFailed is what the caller of an in-place act is told when the act's
// effect failed for a reason that is not theirs: what was being done, and the
// failure in its own words — and no more than words.
//
// The effects run the engine, and what the engine meets on the way has
// classes of its own: a decision with no live version is "not found", a step
// already finished is "invalid". Passed on, those would answer the caller 404
// for an instance that exists, or 400 for a request that was well formed and
// that they cannot correct. By the time an effect runs the caller has been
// let in, the instance found and the plan accepted; whatever fails after that
// is the server's, and is answered as that. So the cause is kept as words and
// deliberately not wrapped, as graphRunBy keeps a missing version.
func effectFailed(doing string, err error) error {
	return fmt.Errorf("%s: %s", doing, err.Error())
}

// cancelWhereItStands ends an instance as it stands: every task it has open
// is withdrawn and whoever held it told, what it was waiting for is dropped,
// and it is cancelled — and the ledger and the trail say who ended it, where
// it stood and why.
//
// It ends the whole instance, whichever step the command names; the step is
// where the record says the instance stood. A command that names none closes
// an instance that waits nowhere, which is what apply asked of the locked row,
// and its row and its entry name no step: none is invented for either.
//
// The record is made from the tasks the effect held and withdrew, and counts
// them. The plan lists no more than a screenful of the instance's open work,
// read without its rows held; who holds a task may have changed since, and
// there may be more of them than it lists.
func (s *instanceDeviationService) cancelWhereItStands(
	ctx context.Context,
	locked models.ProcessInstanceModel,
	plan entities.DeviationPlan,
	command entities.DeviationCommand,
	actor string,
) (entities.Deviation, error) {
	runID, err := uuid.NewV7()
	if err != nil {
		return entities.Deviation{}, err
	}
	ended, withdrawn, err := s.actions.cancel(ctx, locked)
	if err != nil {
		return entities.Deviation{}, effectFailed("cancelling this instance", err)
	}

	row := inPlaceDeviation(locked, plan, command, actor, runID)
	// As a migration's cancel names it: the one task it took, when it took one.
	if len(withdrawn) == 1 {
		row.Task = &entities.Task{ID: uuid.UUID(withdrawn[0].ID)}
	}
	row.Before, row.After = withdrawnTaskValues(withdrawn)
	row.Before["instance"] = map[string]any{"status": string(locked.Status)}
	row.After["instance"] = map[string]any{"status": string(ended.Status)}
	// A count, and no business value: the details of a row are not sealed.
	row.Details = map[string]any{"withdrawn": len(withdrawn)}

	recorded, err := s.actions.record(ctx, row, cancelEntry(locked, plan, command, actor, runID))
	if err != nil {
		return entities.Deviation{}, fmt.Errorf("recording that this instance was cancelled: %w", err)
	}
	return recorded, nil
}

// holdWhereItStands raises an instance as an incident at the step it waits
// at, for somebody to decide, and changes nothing else about it: its tokens,
// its tasks and its status are as they were, and its work can still be done.
//
// A step that already has an incident open keeps that one — the effect raises
// no second — and the hold is recorded all the same, naming it: a hold in
// place is an act made for a visit, by somebody, for a reason, and that is
// what the ledger keeps. The same hold asked for again never gets here; apply
// answers it with its row first.
func (s *instanceDeviationService) holdWhereItStands(
	ctx context.Context,
	locked models.ProcessInstanceModel,
	plan entities.DeviationPlan,
	command entities.DeviationCommand,
	actor string,
) (entities.Deviation, error) {
	runID, err := uuid.NewV7()
	if err != nil {
		return entities.Deviation{}, err
	}
	message := fmt.Sprintf("held at “%s” by %s: %s", plan.NodeName, actor, command.Reason)
	incidentID, _, err := s.actions.hold(ctx, locked, plan.NodeID, message)
	if err != nil {
		return entities.Deviation{}, effectFailed(fmt.Sprintf("holding this instance at “%s”", plan.NodeName), err)
	}

	row := inPlaceDeviation(locked, plan, command, actor, runID)
	row.After = map[string]any{"incident": map[string]any{"id": incidentID.String(), "status": string(models.IncidentOpen)}}

	recorded, err := s.actions.record(ctx, row, holdEntry(locked, plan, command, actor, runID, incidentID))
	if err != nil {
		return entities.Deviation{}, fmt.Errorf("recording that this instance was held at “%s”: %w", plan.NodeName, err)
	}
	return recorded, nil
}

// valuesHeld is what an instance holds under the names a waive is about to
// set: what the record keeps as how things were. A name it holds nothing
// under is left out — the record does not say a value was nothing when there
// was no value.
func valuesHeld(variables, outputs map[string]any) map[string]any {
	held := make(map[string]any, len(outputs))
	for name := range outputs {
		if value, has := variables[name]; has {
			held[name] = value
		}
	}
	return held
}

// inPlaceDeviation is the part of a ledger row every in-place act shares: the
// instance, its project and the version it runs, taken from the locked row;
// the act and its reach; the step, when the command names one; who, why, and
// the visit the act was made for.
func inPlaceDeviation(
	locked models.ProcessInstanceModel,
	plan entities.DeviationPlan,
	command entities.DeviationCommand,
	actor string,
	runID uuid.UUID,
) entities.Deviation {
	row := entities.Deviation{
		Project:    &entities.Project{ID: uuid.UUID(locked.ProjectID)},
		Instance:   &entities.ProcessInstance{ID: uuid.UUID(locked.ID)},
		Definition: &entities.ProcessDefinition{ID: uuid.UUID(locked.DefinitionID)},
		Kind:       plan.Kind,
		Scope:      plan.Scope,
		Origin:     entities.DeviationOriginInPlace,
		Status:     entities.DeviationApplied,
		Actor:      actor,
		Reason:     command.Reason,
		VisitKey:   plan.VisitKey,
		RunID:      runID,
	}
	if plan.NodeID != "" {
		row.Node = &entities.Node{ID: plan.NodeID, Name: plan.NodeName}
	}
	return row
}

// waiveEntry is the trail entry of a step waived in place. It is a
// node_skipped entry, as a migration's skip is, and says in its outcome and
// in its sentence that the step was waived: nobody performed it, and nothing
// about it reads as an approval somebody gave.
//
// It names the values the waiver set and does not carry them: the trail is
// not sealed, and what they were is in the ledger row the entry points at.
func waiveEntry(
	locked models.ProcessInstanceModel,
	plan entities.DeviationPlan,
	command entities.DeviationCommand,
	actor string,
	runID uuid.UUID,
) entities.AuditEntry {
	set := sortedKeys(command.Outputs)
	if set == nil {
		set = []string{}
	}
	return entities.AuditEntry{
		Type:    EventNodeSkipped,
		Message: fmt.Sprintf("waive %s in place", plan.NodeID),
		Narrative: fmt.Sprintf("“%s” was waived — nobody performed it — by %s. Reason: %s.",
			plan.NodeName, actor, command.Reason),
		Timestamp: time.Now(),
		Data: map[string]any{
			"node_id":     plan.NodeID,
			"run_id":      runID.String(),
			"action":      string(servicecontracts.NodeActionSkip),
			"outcome":     outcomeWaived,
			"origin":      string(entities.DeviationOriginInPlace),
			"reason":      command.Reason,
			"actor":       actor,
			"outputs_set": set,
		},
		Project:  &entities.Project{ID: uuid.UUID(locked.ProjectID)},
		Instance: &entities.ProcessInstance{ID: uuid.UUID(locked.ID)},
		Node:     &entities.Node{ID: plan.NodeID, Name: plan.NodeName},
	}
}

// cancelEntry is the trail entry of an instance ended in place. It says where
// the instance stood when the command names a step, and that it stood nowhere
// when it names none: the entry then carries no step and no node_id, as its
// ledger row carries none.
func cancelEntry(
	locked models.ProcessInstanceModel,
	plan entities.DeviationPlan,
	command entities.DeviationCommand,
	actor string,
	runID uuid.UUID,
) entities.AuditEntry {
	entry := entities.AuditEntry{
		Type:    EventInstanceCancelled,
		Message: "cancel in place",
		Narrative: fmt.Sprintf("This instance was ended by %s while it was not waiting at any step. Reason: %s.",
			actor, command.Reason),
		Timestamp: time.Now(),
		Data: map[string]any{
			"run_id": runID.String(),
			"action": string(servicecontracts.NodeActionCancel),
			"origin": string(entities.DeviationOriginInPlace),
			"reason": command.Reason,
			"actor":  actor,
		},
		Project:  &entities.Project{ID: uuid.UUID(locked.ProjectID)},
		Instance: &entities.ProcessInstance{ID: uuid.UUID(locked.ID)},
	}
	if plan.NodeID == "" {
		return entry
	}
	entry.Message = fmt.Sprintf("cancel %s in place", plan.NodeID)
	entry.Narrative = fmt.Sprintf("This instance was ended at “%s” by %s. Reason: %s.", plan.NodeName, actor, command.Reason)
	entry.Data["node_id"] = plan.NodeID
	entry.Node = &entities.Node{ID: plan.NodeID, Name: plan.NodeName}
	return entry
}

// holdEntry is the trail entry of an instance held in place: the step it was
// held at, and the incident that holds it there.
func holdEntry(
	locked models.ProcessInstanceModel,
	plan entities.DeviationPlan,
	command entities.DeviationCommand,
	actor string,
	runID, incidentID uuid.UUID,
) entities.AuditEntry {
	return entities.AuditEntry{
		Type:    EventInstanceHeld,
		Message: fmt.Sprintf("hold %s in place", plan.NodeID),
		Narrative: fmt.Sprintf("This instance was held at “%s” by %s and raised as an incident for somebody to decide. Reason: %s.",
			plan.NodeName, actor, command.Reason),
		Timestamp: time.Now(),
		Data: map[string]any{
			"node_id":     plan.NodeID,
			"run_id":      runID.String(),
			"action":      string(servicecontracts.NodeActionHold),
			"origin":      string(entities.DeviationOriginInPlace),
			"reason":      command.Reason,
			"actor":       actor,
			"incident_id": incidentID.String(),
		},
		Project:  &entities.Project{ID: uuid.UUID(locked.ProjectID)},
		Instance: &entities.ProcessInstance{ID: uuid.UUID(locked.ID)},
		Node:     &entities.Node{ID: plan.NodeID, Name: plan.NodeName},
	}
}
