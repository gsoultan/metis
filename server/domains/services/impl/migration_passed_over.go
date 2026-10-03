package impl

import (
	"fmt"

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
