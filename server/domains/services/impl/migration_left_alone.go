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
// They are made together, here, from the same step ids — one constructor for
// each cause, each calling the sentence that exists for it — so that a cause
// can never be given beside another cause's sentence, and nothing has to
// guess a cause from the words. Each takes the source version as the run read
// it once (sourceSteps): no reason looks through the definition again.
type leftAlone struct {
	cause entities.PassedOverCause
	// steps is the first of the steps the cause is about, and stepsInAll how
	// many there were (stepsOf).
	steps      []entities.PassedOverStep
	stepsInAll int
	reason     string
}

// none reports that there is no cause: the instance may be moved.
func (w leftAlone) none() bool { return w.cause == "" }

// stepsOf is the steps a cause is about, as the run lists them, and how
// many there were: the first entities.MaxPassedOverSteps of them, in the
// order given, each by its id and by the name the source version gives it —
// the id where it gives none, as the sentence beside it names the step.
// Nothing, of none, for a cause about no step.
//
// What it lists has a size. An instance can hold work on as many steps as
// its version has, and a step's id and name are as long as the author of the
// definition made them: the list stops at ten and says how many it left out,
// and each id and each name is kept to the length a step's name is kept to
// wherever one is recorded or shown (shownStepName). The sentence beside it
// names the same steps and is cut the same way (sourceSteps.quoted).
func stepsOf(source sourceSteps, nodeIDs []string) (listed []entities.PassedOverStep, inAll int) {
	if len(nodeIDs) == 0 {
		return nil, 0
	}
	shown := nodeIDs[:min(len(nodeIDs), entities.MaxPassedOverSteps)]
	listed = make([]entities.PassedOverStep, 0, len(shown))
	for _, id := range shown {
		listed = append(listed, entities.PassedOverStep{NodeID: shownStepName(id), Name: shownStepName(source.name(id))})
	}
	return listed, len(nodeIDs)
}

// about is a reason that is about steps: the cause, those steps as stepsOf
// lists them, and the sentence.
func about(cause entities.PassedOverCause, source sourceSteps, nodeIDs []string, reason string) leftAlone {
	why := leftAlone{cause: cause, reason: reason}
	why.steps, why.stepsInAll = stepsOf(source, nodeIDs)
	return why
}

// becauseItLeftTheStep: a decision found the instance gone from nodeID.
func becauseItLeftTheStep(source sourceSteps, nodeID string) leftAlone {
	return about(entities.PassedOverLeftTheStep, source, []string{nodeID}, source.leftTheStep(nodeID))
}

// becauseItStopped: the instance finished, or was ended, before its lock.
func becauseItStopped(source sourceSteps) leftAlone {
	return leftAlone{cause: entities.PassedOverNoLongerRunning, reason: source.noLongerRunning()}
}

// becauseNotPlannedFor: the plan was not made for the instance.
func becauseNotPlannedFor(source sourceSteps) leftAlone {
	return leftAlone{cause: entities.PassedOverNotPlannedFor, reason: source.notPlannedFor()}
}

// becauseAlreadyMoved: another run had moved the instance off the version.
func becauseAlreadyMoved(source sourceSteps) leftAlone {
	return leftAlone{cause: entities.PassedOverAlreadyMoved, reason: source.alreadyMoved()}
}

// becauseNowhereToLand: the instance holds work on nodeIDs, and the new
// version has nowhere to put it.
func becauseNowhereToLand(source sourceSteps, target models.ProcessDefinitionModel, nodeIDs []string) leftAlone {
	return about(entities.PassedOverNowhereToLand, source, nodeIDs, source.nowhereToLand(target, nodeIDs))
}

// becauseNothingDecidesThere: the instance has a task or a waiting event on
// nodeIDs, which the migration decides, and no token there.
func becauseNothingDecidesThere(source sourceSteps, target models.ProcessDefinitionModel, nodeIDs []string) leftAlone {
	return about(entities.PassedOverLeftWhereNothingDecides, source, nodeIDs, source.leftWhereNothingDecides(target, nodeIDs))
}

// becauseUndecided: the instance waits on nodeIDs, which the migration
// decides, and no decision settled it.
func becauseUndecided(source sourceSteps, nodeIDs []string) leftAlone {
	return about(entities.PassedOverWaitingToBeDecided, source, nodeIDs, source.waitingToBeDecided(nodeIDs))
}

// becauseCountersWouldMerge: the mapping puts two steps the instance is
// part-way through onto one.
func becauseCountersWouldMerge(source sourceSteps) leftAlone {
	return leftAlone{cause: entities.PassedOverCountersWouldMerge, reason: source.countersWouldMerge()}
}
