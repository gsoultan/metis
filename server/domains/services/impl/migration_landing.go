package impl

import (
	"context"
	"fmt"
	"slices"

	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// whyNotMoved says why an instance must be left on the version it is running —
// the cause, the steps it is about and the sentence — and nothing (the zero
// value) when it may be moved. It is asked of the row the rewrite's lock
// returned, inside the rewrite's transaction, before anything is written.
//
// The planner answers the same question — can everything this instance holds
// land on the new version — but from its listing, and the listing is as old as
// the run. An instance that reaches, between the listing and its lock, a step
// the new version does not have was re-pointed there all the same: a token and
// an open task on a step nothing follows. Its holder could still complete the
// task; the token then came off, and the instance stayed active for ever with
// nothing left that could move it, cancel it or hold it.
//
// So this is the planner's own check, survey, over the one locked row, given
// what the plan was given. An instance that has not moved since the listing
// passed it in the plan and passes it again here, and is rewritten exactly as
// before. Deliberately not a second predicate: two would drift, and a dry run
// would then call fine what the apply leaves behind.
//
// One thing is asked that survey does not ask. survey lets work on a step the
// migration decides stand without anywhere to land, because that work is to be
// skipped, cancelled or held, not moved. That is true in the plan and no
// longer true here: the decisions have been taken by now, each in its own
// transaction, and a token still on a decided step is one no decision settled
// — the instance reached the step after its work was decided, or a skip left
// part of a repeating step behind. Moving it would carry it past a decision
// the plan says is somebody's to make.
//
// The same goes for what is not a token. A decision acts on the instances that
// hold a token on its step, so an open task or a waiting event on a decided
// step with no token under it is settled by nothing; where the new version has
// no such step it is not carried there. A job is left out on purpose: a timer
// cannot be deleted, so a skipped wait leaves its timer behind, pending, on
// the step the instance has just left, and the engine dismisses it when it
// comes due because no token waits for it. Holding the instance back for that
// row would hold it until the timer's hour came round.
func (s *migrationService) whyNotMoved(
	ctx context.Context,
	locked models.ProcessInstanceModel,
	source, target models.ProcessDefinitionModel,
	targetNodes map[string]models.FlowNode,
	nodeMapping map[string]string,
	actions map[string]servicecontracts.NodeAction,
) (leftAlone, error) {
	if undecided := undecidedSteps(locked, actions); len(undecided) > 0 {
		return becauseUndecided(source, undecided), nil
	}
	found, err := s.survey(ctx, []models.ProcessInstanceModel{locked}, targetNodes, nodeMapping, actions)
	if err != nil {
		return leftAlone{}, fmt.Errorf("checking where the work of instance %s would land: %w", locked.ID, err)
	}
	if nowhere := nodesOf(found.unlandable, found.stateStranded); len(nowhere) > 0 {
		return becauseNowhereToLand(source, target, nowhere), nil
	}
	// No token is on a decided step by now, so a task or a waiting event still
	// on one is work no decision can reach, and the plan's exemption would
	// carry it to a version that has no such step.
	if len(found.leftOnDecided) > 0 {
		return becauseNothingDecidesThere(source, target, found.leftOnDecided), nil
	}
	if len(found.stateCollisions) > 0 {
		return becauseCountersWouldMerge(source), nil
	}
	return leftAlone{}, nil
}

// undecidedSteps are the steps the migration decides that the instance still
// holds a token on, in the order the decisions are taken in.
func undecidedSteps(instance models.ProcessInstanceModel, actions map[string]servicecontracts.NodeAction) []string {
	var steps []string
	for _, nodeID := range sortedKeys(actions) {
		if holdsWork(instance, nodeID) {
			steps = append(steps, nodeID)
		}
	}
	return steps
}

// nodesOf is the node ids of several lists as one, each once, in order.
func nodesOf(lists ...[]string) []string {
	all := slices.Concat(lists...)
	slices.Sort(all)
	return slices.Compact(all)
}
