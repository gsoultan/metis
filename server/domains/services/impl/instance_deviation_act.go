package impl

import (
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
	case entities.DeviationCancel, entities.DeviationHold:
		// Planned and previewed; not yet made in place.
		return entities.Deviation{}, apierr.Invalidf("a %s cannot be applied in place yet; preview it with dry_run", command.Kind)
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
// recorded.
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
		return entities.Deviation{}, fmt.Errorf("waiving “%s”: %w", plan.NodeName, err)
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
