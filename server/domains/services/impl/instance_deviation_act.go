package impl

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// act makes the change a command asks for and records it, and answers the
// ledger row it wrote.
//
// Whether to act has been decided: its caller holds the instance's lock and
// has asked the locked row everything there is to ask. locked and live are
// that one row, as the store holds it and as the engine reads it; def is the
// graph it runs and plan the plan made from it. The change, the row and the
// trail entry are written in the caller's unit of work, so they are kept
// together or not at all.
//
// It cancels and it holds. It does not waive: a waive is asked for (request)
// and made by the approval of a second administrator (approveWaive), and
// there is no path by which the request that asks for one makes it. Handed a
// waive all the same, it refuses as the server's own mistake.
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
		return entities.Deviation{}, fmt.Errorf(
			"acting on instance %s: a waive is never made on one administrator's call; it waits for a second administrator, and only an approval makes it", live.ID)
	case entities.DeviationCancel:
		return s.cancelWhereItStands(ctx, locked, plan, command, actor)
	case entities.DeviationHold:
		return s.holdWhereItStands(ctx, locked, plan, command, actor)
	}
	return entities.Deviation{}, fmt.Errorf("acting on instance %s: %q is not something done to an instance in place", live.ID, command.Kind)
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
//
// Writing the record is told the same way. The ledger refuses a reason that is
// missing or too long as the caller's to fix, but by the time an act records
// the plan has refused both by the same rule (refuseWhereItStands), so a
// refusal from the ledger here is not something the caller can put right.
func effectFailed(doing string, err error) error {
	return fmt.Errorf("%s: %s", doing, err.Error())
}

// maxRecordedInPlace is how many withdrawn tasks the ledger row of a waive or
// a cancel in place names one by one, and how many closed incidents a
// cancel's does. A row is read whole
// — by the ledger's own route, and by whoever asks what was done to an
// instance — and how much an instance has open is the instance's to say: a
// step done once for each of a thousand people has a thousand tasks. The row
// counts them all.
const maxRecordedInPlace = 200

// lowestByID is the first limit of tasks in the order of their ids, or all of
// them when there are no more than that. The tasks are not reordered where
// they are: the effect returns them newest first, and its other caller reads
// them so.
func lowestByID(tasks []models.TaskModel, limit int) []models.TaskModel {
	if len(tasks) <= limit {
		return tasks
	}
	ordered := slices.Clone(tasks)
	slices.SortFunc(ordered, func(a, b models.TaskModel) int { return byID(uuid.UUID(a.ID), uuid.UUID(b.ID)) })
	return ordered[:limit]
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
