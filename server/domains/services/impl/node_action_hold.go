package impl

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/repositories/models"
)

// hold raises the incident that holds an instance at nodeID, saying message,
// and reports whether it raised one: an instance already held there keeps its
// one open incident, whose id is answered with raised false.
//
// locked is the row its caller locked. Nothing about the instance is moved,
// cancelled or advanced: a hold only changes where it is visible.
func (a nodeActions) hold(
	ctx context.Context,
	locked models.ProcessInstanceModel,
	nodeID, message string,
) (incidentID uuid.UUID, raised bool, err error) {
	// A held instance stays where it is, so whoever decided to hold it once
	// can find it again and decide the same. Raising a second incident for the
	// same node would turn one instance somebody has to look at into a growing
	// pile of identical rows, which is the fastest way to make an inbox worth
	// ignoring.
	open, err := a.repo.Incident().ListByInstance(ctx, uuid.UUID(locked.ID))
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("reading the incidents already on instance %s: %w", locked.ID, err)
	}
	for _, existing := range open {
		if existing.NodeID == nodeID && existing.Status == models.IncidentOpen {
			return uuid.UUID(existing.ID), false, nil
		}
	}

	incident := models.IncidentModel{
		InstanceID:   locked.ID,
		DefinitionID: locked.DefinitionID,
		NodeID:       nodeID,
		Status:       models.IncidentOpen,
		Error:        message,
	}
	incident.ID = models.UUID(uuid.New())
	created, err := a.repo.Incident().Create(ctx, incident)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("raising the incident that holds instance %s: %w", locked.ID, err)
	}
	return uuid.UUID(created.ID), true, nil
}
