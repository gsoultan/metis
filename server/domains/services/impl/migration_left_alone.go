package impl

import (
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// leftAlone is why a run leaves an instance on the version it is running: the
// cause as a code, the steps the cause is about, and the sentence the reply
// has always carried. The zero value is no reason at all: the instance may be
// moved.
//
// The three are made together, here, from the same step ids — one constructor
// for each cause, each calling the sentence that exists for it — so that a
// cause can never be given beside another cause's sentence, and nothing has to
// guess a cause from the words.
type leftAlone struct {
	cause  entities.PassedOverCause
	steps  []entities.PassedOverStep
	reason string
}

// none reports that there is no cause: the instance may be moved.
func (w leftAlone) none() bool { return w.cause == "" }

// stepsOf is the steps a cause is about, in the order given: each by its id
// and by the name the source version gives it — the id where it gives none,
// as the sentence beside it names the step. Nothing for a cause about no step.
func stepsOf(source models.ProcessDefinitionModel, nodeIDs []string) []entities.PassedOverStep {
	if len(nodeIDs) == 0 {
		return nil
	}
	steps := make([]entities.PassedOverStep, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		steps = append(steps, entities.PassedOverStep{NodeID: id, Name: nodeNameIn(source.Nodes, id)})
	}
	return steps
}

// becauseItLeftTheStep: a decision found the instance gone from nodeID.
func becauseItLeftTheStep(source models.ProcessDefinitionModel, nodeID string) leftAlone {
	return leftAlone{entities.PassedOverLeftTheStep, stepsOf(source, []string{nodeID}), leftTheStep(source, nodeID)}
}

// becauseItStopped: the instance finished, or was ended, before its lock.
func becauseItStopped(source models.ProcessDefinitionModel) leftAlone {
	return leftAlone{entities.PassedOverNoLongerRunning, nil, noLongerRunning(source)}
}

// becauseNotPlannedFor: the plan was not made for the instance.
func becauseNotPlannedFor(source models.ProcessDefinitionModel) leftAlone {
	return leftAlone{entities.PassedOverNotPlannedFor, nil, notPlannedFor(source)}
}

// becauseAlreadyMoved: another run had moved the instance off the version.
func becauseAlreadyMoved(source models.ProcessDefinitionModel) leftAlone {
	return leftAlone{entities.PassedOverAlreadyMoved, nil, alreadyMoved(source)}
}

// becauseNowhereToLand: the instance holds work on nodeIDs, and the new
// version has nowhere to put it.
func becauseNowhereToLand(source, target models.ProcessDefinitionModel, nodeIDs []string) leftAlone {
	return leftAlone{entities.PassedOverNowhereToLand, stepsOf(source, nodeIDs), nowhereToLand(source, target, nodeIDs)}
}

// becauseNothingDecidesThere: the instance has a task or a waiting event on
// nodeIDs, which the migration decides, and no token there.
func becauseNothingDecidesThere(source, target models.ProcessDefinitionModel, nodeIDs []string) leftAlone {
	return leftAlone{entities.PassedOverLeftWhereNothingDecides, stepsOf(source, nodeIDs), leftWhereNothingDecides(source, target, nodeIDs)}
}

// becauseUndecided: the instance waits on nodeIDs, which the migration
// decides, and no decision settled it.
func becauseUndecided(source models.ProcessDefinitionModel, nodeIDs []string) leftAlone {
	return leftAlone{entities.PassedOverWaitingToBeDecided, stepsOf(source, nodeIDs), waitingToBeDecided(source, nodeIDs)}
}

// becauseCountersWouldMerge: the mapping puts two steps the instance is
// part-way through onto one.
func becauseCountersWouldMerge(source models.ProcessDefinitionModel) leftAlone {
	return leftAlone{entities.PassedOverCountersWouldMerge, nil, countersWouldMerge(source)}
}
