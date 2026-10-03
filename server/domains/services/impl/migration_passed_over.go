package impl

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// passedOver is the run's account of an instance it left alone.
func passedOver(instance models.ProcessInstanceModel, reason string) entities.PassedOverInstance {
	return entities.PassedOverInstance{
		Instance: &entities.ProcessInstance{ID: uuid.UUID(instance.ID)},
		Reason:   reason,
	}
}

// leftTheStep is why a skip, a cancel or a hold did nothing to an instance: it
// found the instance, once locked, no longer on the step. The step is named as
// the source version names it, for the person reading the reply.
func leftTheStep(source models.ProcessDefinitionModel, nodeID string) string {
	return fmt.Sprintf("It was no longer waiting at %q when the migration reached it, so nothing was decided there "+
		"and it was not moved. It stays on version %d; if it is still running, run the same migration again "+
		"to plan for where it now stands.", nodeNameIn(source.Nodes, nodeID), source.Version)
}

// noLongerRunning is why an instance was not moved: it finished, or was ended,
// between the run listing it and locking it.
func noLongerRunning(source models.ProcessDefinitionModel) string {
	return fmt.Sprintf("It was no longer running when the migration reached it, so it was not moved. "+
		"It stays on version %d, the one it ran on.", source.Version)
}

// nowhereToLand is why an instance was not moved: once locked, it held work —
// a token, a task, a timer, a waiting event or a counter — on steps the new
// version has no step for and the mapping does not cover. The planner refuses
// a migration for exactly this; the instance got there after the plan was
// made, by itself or because a skip advanced it there.
func nowhereToLand(source, target models.ProcessDefinitionModel, nodeIDs []string) string {
	return fmt.Sprintf("When the migration came to move it, it had work at %s, and version %d has nowhere to put "+
		"that, so it was not moved. It stays on version %d. Plan the migration again for where it now stands: "+
		"it needs a mapping, or a decision, for that work.",
		stepNames(source, nodeIDs), target.Version, source.Version)
}

// waitingToBeDecided is why an instance was not moved: once locked, it had a
// token on a step this migration decides rather than moves, and no decision had
// settled it. It reached the step after its work was decided, or a skip left
// part of the step behind.
func waitingToBeDecided(source models.ProcessDefinitionModel, nodeIDs []string) string {
	return fmt.Sprintf("When the migration came to move it, it was waiting at %s, where this migration decides the "+
		"work rather than moving it, and no decision had settled it, so it was not moved. It stays on version %d; "+
		"run the same migration again to decide it where it now stands.",
		stepNames(source, nodeIDs), source.Version)
}

// countersWouldMerge is why an instance was not moved: once locked, it held
// progress counters on two steps the mapping puts onto one.
func countersWouldMerge(source models.ProcessDefinitionModel) string {
	return fmt.Sprintf("When the migration came to move it, it was part-way through two steps that this mapping "+
		"moves onto one, and their progress cannot be added together, so it was not moved. It stays on version %d. "+
		"Plan the migration again for where it now stands: it needs those steps mapped apart.", source.Version)
}

// stepNames names steps as the source version names them, quoted, for a
// sentence: "A", "A" and "B", or "A", "B" and "C".
func stepNames(source models.ProcessDefinitionModel, nodeIDs []string) string {
	names := make([]string, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		names = append(names, fmt.Sprintf("%q", nodeNameIn(source.Nodes, id)))
	}
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
